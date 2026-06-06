package core

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	ledgerIntegrityAlgorithm = "hmac-sha256-chain-v1"
	ledgerIntegrityKeyFile   = "ledger-integrity-key.json"
	ledgerIntegrityKeyBytes  = 32

	integrityStatusVerified       = "verified"
	integrityStatusFailed         = "failed"
	integrityStatusLegacyUnsigned = "legacy_unsigned"
	integrityStatusEmpty          = "empty"
)

type ledgerIntegrityKey struct {
	SchemaVersion int    `json:"schema_version"`
	KeyID         string `json:"key_id"`
	Secret        string `json:"secret"`
	CreatedAt     string `json:"created_at"`
}

func (s *Service) appendSignedInstallRecord(record InstallRecord) (InstallRecord, error) {
	signed, err := s.signInstallRecord(record)
	if err != nil {
		return InstallRecord{}, err
	}
	if err := appendJSONLine(s.installRecordsPath(), signed); err != nil {
		return InstallRecord{}, err
	}
	if err := s.writeLedgerHead(installRecordsFile, signed.Integrity); err != nil {
		return InstallRecord{}, err
	}
	return signed, nil
}

func (s *Service) ensureCommerceLedgersAppendable() error {
	if err := ensureLedgerSummaryAppendable(s.verifyInstallLedger()); err != nil {
		return err
	}
	return ensureLedgerSummaryAppendable(s.verifyBillingLedger())
}

func ensureLedgerSummaryAppendable(summary LedgerIntegritySummary, err error) error {
	if err != nil {
		return err
	}
	if summary.Failed > 0 || summary.Status == integrityStatusFailed {
		return NewError(CodeIntegrityFailed, "local commerce ledger integrity failed; refusing to append trusted record", summary)
	}
	return nil
}

func (s *Service) appendSignedBillingRecords(records []BillingRecord) ([]BillingRecord, error) {
	if len(records) == 0 {
		return nil, nil
	}
	signed := make([]BillingRecord, 0, len(records))
	for _, record := range records {
		item, err := s.signBillingRecord(record)
		if err != nil {
			return nil, err
		}
		if err := appendJSONLine(s.billingRecordsPath(), item); err != nil {
			return nil, err
		}
		if err := s.writeLedgerHead(billingRecordsFile, item.Integrity); err != nil {
			return nil, err
		}
		signed = append(signed, item)
	}
	return signed, nil
}

func (s *Service) signInstallRecord(record InstallRecord) (InstallRecord, error) {
	key, err := s.loadOrCreateLedgerIntegrityKey()
	if err != nil {
		return InstallRecord{}, err
	}
	summary, err := s.verifyInstallLedger()
	if err != nil {
		return InstallRecord{}, err
	}
	if summary.Failed > 0 {
		return InstallRecord{}, NewError(CodeIntegrityFailed, "install ledger integrity failed; refusing to append trusted record", summary)
	}
	record.Integrity = nil
	hash, err := recordIntegrityHash(key, installRecordsFile, summary.LastHash, record)
	if err != nil {
		return InstallRecord{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	record.Integrity = &RecordIntegrity{
		Algorithm:    ledgerIntegrityAlgorithm,
		Ledger:       installRecordsFile,
		KeyID:        key.KeyID,
		Sequence:     summary.Records + 1,
		PreviousHash: summary.LastHash,
		Hash:         hash,
		SignedAt:     now,
		Status:       integrityStatusVerified,
		HeadHash:     hash,
		HeadMatched:  true,
	}
	return record, nil
}

func (s *Service) signBillingRecord(record BillingRecord) (BillingRecord, error) {
	key, err := s.loadOrCreateLedgerIntegrityKey()
	if err != nil {
		return BillingRecord{}, err
	}
	summary, err := s.verifyBillingLedger()
	if err != nil {
		return BillingRecord{}, err
	}
	if summary.Failed > 0 {
		return BillingRecord{}, NewError(CodeIntegrityFailed, "billing ledger integrity failed; refusing to append trusted record", summary)
	}
	record.Integrity = nil
	hash, err := recordIntegrityHash(key, billingRecordsFile, summary.LastHash, record)
	if err != nil {
		return BillingRecord{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	record.Integrity = &RecordIntegrity{
		Algorithm:    ledgerIntegrityAlgorithm,
		Ledger:       billingRecordsFile,
		KeyID:        key.KeyID,
		Sequence:     summary.Records + 1,
		PreviousHash: summary.LastHash,
		Hash:         hash,
		SignedAt:     now,
		Status:       integrityStatusVerified,
		HeadHash:     hash,
		HeadMatched:  true,
	}
	return record, nil
}

func (s *Service) readInstallRecordsWithIntegrity() ([]InstallRecord, LedgerIntegritySummary, error) {
	var records []InstallRecord
	if err := readJSONLines(s.installRecordsPath(), &records); err != nil {
		return nil, LedgerIntegritySummary{}, err
	}
	summary, records, err := s.verifyInstallRecords(records)
	if err != nil {
		return nil, LedgerIntegritySummary{}, err
	}
	return records, summary, nil
}

func (s *Service) readBillingRecordsWithIntegrity() ([]BillingRecord, LedgerIntegritySummary, error) {
	var records []BillingRecord
	if err := readJSONLines(s.billingRecordsPath(), &records); err != nil {
		return nil, LedgerIntegritySummary{}, err
	}
	summary, records, err := s.verifyBillingRecords(records)
	if err != nil {
		return nil, LedgerIntegritySummary{}, err
	}
	return records, summary, nil
}

func (s *Service) verifyInstallLedger() (LedgerIntegritySummary, error) {
	var records []InstallRecord
	if err := readJSONLines(s.installRecordsPath(), &records); err != nil {
		return LedgerIntegritySummary{}, err
	}
	summary, _, err := s.verifyInstallRecords(records)
	return summary, err
}

func (s *Service) verifyBillingLedger() (LedgerIntegritySummary, error) {
	var records []BillingRecord
	if err := readJSONLines(s.billingRecordsPath(), &records); err != nil {
		return LedgerIntegritySummary{}, err
	}
	summary, _, err := s.verifyBillingRecords(records)
	return summary, err
}

func (s *Service) verifyInstallRecords(records []InstallRecord) (LedgerIntegritySummary, []InstallRecord, error) {
	key, err := s.loadLedgerIntegrityKeyIfPresent()
	if err != nil {
		return LedgerIntegritySummary{}, nil, err
	}
	head, err := s.readLedgerHead(installRecordsFile)
	if err != nil {
		return LedgerIntegritySummary{}, nil, err
	}
	summary := newLedgerIntegritySummary(installRecordsFile, key, head, len(records))
	lastHash := ""
	for index := range records {
		record := records[index]
		result := verifyRecordIntegrity(key, installRecordsFile, head, index+1, lastHash, record, record.Integrity)
		records[index].Integrity = result
		applyIntegrityResult(&summary, result)
		if result.Hash != "" {
			lastHash = result.Hash
		}
	}
	return finalizeLedgerIntegritySummary(summary, lastHash, head), records, nil
}

func (s *Service) verifyBillingRecords(records []BillingRecord) (LedgerIntegritySummary, []BillingRecord, error) {
	key, err := s.loadLedgerIntegrityKeyIfPresent()
	if err != nil {
		return LedgerIntegritySummary{}, nil, err
	}
	head, err := s.readLedgerHead(billingRecordsFile)
	if err != nil {
		return LedgerIntegritySummary{}, nil, err
	}
	summary := newLedgerIntegritySummary(billingRecordsFile, key, head, len(records))
	lastHash := ""
	for index := range records {
		record := records[index]
		result := verifyRecordIntegrity(key, billingRecordsFile, head, index+1, lastHash, record, record.Integrity)
		records[index].Integrity = result
		applyIntegrityResult(&summary, result)
		if result.Hash != "" {
			lastHash = result.Hash
		}
	}
	return finalizeLedgerIntegritySummary(summary, lastHash, head), records, nil
}

func verifyRecordIntegrity(key *ledgerIntegrityKey, ledger string, head *RecordIntegrity, sequence int, previousHash string, record any, existing *RecordIntegrity) *RecordIntegrity {
	now := time.Now().UTC().Format(time.RFC3339)
	if existing == nil || strings.TrimSpace(existing.Hash) == "" {
		return &RecordIntegrity{
			Algorithm:   ledgerIntegrityAlgorithm,
			Ledger:      ledger,
			Sequence:    sequence,
			VerifiedAt:  now,
			Status:      integrityStatusLegacyUnsigned,
			Reason:      "record has no local integrity signature",
			HeadHash:    headHash(head),
			HeadMatched: false,
		}
	}
	result := *existing
	result.VerifiedAt = now
	result.Status = integrityStatusVerified
	result.Reason = ""
	result.HeadHash = headHash(head)
	result.HeadMatched = strings.TrimSpace(result.Hash) != "" && strings.TrimSpace(result.Hash) == headHash(head)
	if result.Algorithm != ledgerIntegrityAlgorithm {
		result.Status = integrityStatusFailed
		result.Reason = "unsupported integrity algorithm"
		return &result
	}
	if result.Ledger != ledger {
		result.Status = integrityStatusFailed
		result.Reason = "ledger mismatch"
		return &result
	}
	if result.Sequence != sequence {
		result.Status = integrityStatusFailed
		result.Reason = "sequence mismatch"
		return &result
	}
	if result.PreviousHash != previousHash {
		result.Status = integrityStatusFailed
		result.Reason = "previous hash mismatch"
		return &result
	}
	if key == nil {
		result.Status = integrityStatusFailed
		result.Reason = "local integrity key is missing"
		return &result
	}
	if result.KeyID != key.KeyID {
		result.Status = integrityStatusFailed
		result.Reason = "integrity key mismatch"
		return &result
	}
	expected, err := recordIntegrityHash(*key, ledger, previousHash, record)
	if err != nil {
		result.Status = integrityStatusFailed
		result.Reason = err.Error()
		return &result
	}
	if !hmac.Equal([]byte(expected), []byte(result.Hash)) {
		result.Status = integrityStatusFailed
		result.Reason = "record hash mismatch"
	}
	return &result
}

func recordIntegrityHash(key ledgerIntegrityKey, ledger, previousHash string, record any) (string, error) {
	data, err := canonicalRecordIntegrityPayload(ledger, previousHash, record)
	if err != nil {
		return "", err
	}
	secret, err := hex.DecodeString(strings.TrimSpace(key.Secret))
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(data)
	return hex.EncodeToString(mac.Sum(nil)), nil
}

func canonicalRecordIntegrityPayload(ledger, previousHash string, record any) ([]byte, error) {
	data, err := json.Marshal(recordWithoutIntegrity(record))
	if err != nil {
		return nil, err
	}
	payload := map[string]any{
		"algorithm":     ledgerIntegrityAlgorithm,
		"ledger":        ledger,
		"previous_hash": previousHash,
		"record":        json.RawMessage(data),
	}
	return json.Marshal(payload)
}

func recordWithoutIntegrity(record any) any {
	switch value := record.(type) {
	case InstallRecord:
		value.Integrity = nil
		return value
	case BillingRecord:
		value.Integrity = nil
		return value
	default:
		return record
	}
}

func newLedgerIntegritySummary(ledger string, key *ledgerIntegrityKey, head *RecordIntegrity, records int) LedgerIntegritySummary {
	summary := LedgerIntegritySummary{
		Ledger:      ledger,
		Algorithm:   ledgerIntegrityAlgorithm,
		Status:      integrityStatusEmpty,
		Records:     records,
		HeadHash:    headHash(head),
		HeadMatched: records == 0 && headHash(head) == "",
		VerifiedAt:  time.Now().UTC().Format(time.RFC3339),
	}
	if key != nil {
		summary.KeyID = key.KeyID
	}
	return summary
}

func applyIntegrityResult(summary *LedgerIntegritySummary, result *RecordIntegrity) {
	switch result.Status {
	case integrityStatusVerified:
		summary.Verified++
	case integrityStatusLegacyUnsigned:
		summary.LegacyUnsigned++
	default:
		summary.Failed++
		if strings.TrimSpace(summary.Reason) == "" {
			summary.Reason = result.Reason
		}
	}
}

func finalizeLedgerIntegritySummary(summary LedgerIntegritySummary, lastHash string, head *RecordIntegrity) LedgerIntegritySummary {
	summary.LastHash = lastHash
	headValue := headHash(head)
	summary.HeadHash = headValue
	summary.HeadMatched = summary.Records == 0 && headValue == "" || summary.Records > 0 && lastHash != "" && lastHash == headValue
	switch {
	case summary.Records == 0 && headValue == "":
		summary.Status = integrityStatusEmpty
	case summary.Failed > 0:
		summary.Status = integrityStatusFailed
	case summary.Verified > 0 && !summary.HeadMatched:
		summary.Status = integrityStatusFailed
		if strings.TrimSpace(summary.Reason) == "" {
			summary.Reason = "ledger head mismatch"
		}
	case summary.LegacyUnsigned > 0:
		summary.Status = integrityStatusLegacyUnsigned
	case !summary.HeadMatched:
		summary.Status = integrityStatusFailed
		if strings.TrimSpace(summary.Reason) == "" {
			summary.Reason = "ledger head mismatch"
		}
	default:
		summary.Status = integrityStatusVerified
	}
	return summary
}

func headHash(head *RecordIntegrity) string {
	if head == nil {
		return ""
	}
	return strings.TrimSpace(head.Hash)
}

func (s *Service) loadOrCreateLedgerIntegrityKey() (ledgerIntegrityKey, error) {
	if key, err := s.loadLedgerIntegrityKeyIfPresent(); err != nil {
		return ledgerIntegrityKey{}, err
	} else if key != nil {
		return *key, nil
	}
	random := make([]byte, ledgerIntegrityKeyBytes)
	if _, err := rand.Read(random); err != nil {
		return ledgerIntegrityKey{}, err
	}
	key := ledgerIntegrityKey{
		SchemaVersion: 1,
		KeyID:         "ledger-key-" + NewTraceID(),
		Secret:        hex.EncodeToString(random),
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.MarshalIndent(key, "", "  ")
	if err != nil {
		return ledgerIntegrityKey{}, err
	}
	if err := writeFileAtomic(s.ledgerIntegrityKeyPath(), append(data, '\n'), 0o600); err != nil {
		return ledgerIntegrityKey{}, err
	}
	return key, nil
}

func (s *Service) loadLedgerIntegrityKeyIfPresent() (*ledgerIntegrityKey, error) {
	data, err := readFileLimited(s.ledgerIntegrityKeyPath(), defaultRecordMaxBytes, "ledger integrity key")
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var key ledgerIntegrityKey
	if err := decodeJSONStrict(data, &key); err != nil {
		return nil, NewError(CodeInvalidArgument, "invalid ledger integrity key", map[string]any{"path": s.ledgerIntegrityKeyPath(), "error": err.Error()})
	}
	if key.SchemaVersion != 1 || strings.TrimSpace(key.KeyID) == "" || strings.TrimSpace(key.Secret) == "" {
		return nil, NewError(CodeIntegrityFailed, "invalid ledger integrity key", map[string]any{"path": s.ledgerIntegrityKeyPath()})
	}
	secret, err := hex.DecodeString(strings.TrimSpace(key.Secret))
	if err != nil || len(secret) < ledgerIntegrityKeyBytes {
		return nil, NewError(CodeIntegrityFailed, "invalid ledger integrity key secret", map[string]any{"path": s.ledgerIntegrityKeyPath()})
	}
	return &key, nil
}

func (s *Service) writeLedgerHead(ledger string, integrity *RecordIntegrity) error {
	if integrity == nil {
		return nil
	}
	head := *integrity
	head.Status = ""
	head.Reason = ""
	head.VerifiedAt = ""
	head.HeadHash = ""
	head.HeadMatched = false
	data, err := json.MarshalIndent(head, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(s.ledgerHeadPath(ledger), append(data, '\n'), 0o600)
}

func (s *Service) readLedgerHead(ledger string) (*RecordIntegrity, error) {
	data, err := readFileLimited(s.ledgerHeadPath(ledger), defaultRecordMaxBytes, "ledger head")
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var head RecordIntegrity
	if err := decodeJSONStrict(bytes.TrimSpace(data), &head); err != nil {
		return nil, NewError(CodeInvalidArgument, "invalid ledger head", map[string]any{"path": s.ledgerHeadPath(ledger), "error": err.Error()})
	}
	return &head, nil
}

func (s *Service) ledgerIntegrityKeyPath() string {
	return filepath.Join(s.Paths.ConfigDir, ledgerIntegrityKeyFile)
}

func (s *Service) ledgerHeadPath(ledger string) string {
	return filepath.Join(s.Paths.ConfigDir, ledger+".head.json")
}
