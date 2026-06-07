package core

import (
	"context"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	commerceReceiptAlgorithm      = "ed25519-commerce-receipt-v1"
	commerceReceiptStatusReceived = "server_received"
	commerceReceiptTrustFile      = "commerce-receipt-trust.json"
	commerceReceiptTrustAlgorithm = "hmac-sha256-receipt-trust-v1"
	commerceReceiptTrustUnbound   = "unbound"
	commerceReceiptTrustBound     = "bound"
	commerceReceiptTrustFailed    = "failed"
)

type commerceProofSubmitRequest struct {
	SchemaVersion int                             `json:"schema_version"`
	ClientVersion string                          `json:"client_version"`
	SubmittedAt   string                          `json:"submitted_at"`
	Proof         CommerceProof                   `json:"proof"`
	Verification  CommerceProofVerificationResult `json:"verification"`
}

type commerceProofSubmitResponse struct {
	OK      bool            `json:"ok,omitempty"`
	Receipt CommerceReceipt `json:"receipt"`
}

type commerceReceiptTrustState struct {
	SchemaVersion    int    `json:"schema_version"`
	Status           string `json:"status"`
	Issuer           string `json:"issuer,omitempty"`
	KeyID            string `json:"key_id"`
	PublicKey        string `json:"public_key"`
	ReceiptAlgorithm string `json:"receipt_algorithm"`
	BoundAt          string `json:"bound_at"`
	LastSeenAt       string `json:"last_seen_at"`
	Algorithm        string `json:"algorithm"`
	LocalKeyID       string `json:"local_key_id"`
	Hash             string `json:"hash"`
}

func (s *Service) SubmitCommerceProof(ctx context.Context, challenge string) (CommerceReceiptSubmitResult, error) {
	var result CommerceReceiptSubmitResult
	err := s.withMutationLock(func() error {
		proof, err := s.CommerceProof(challenge)
		if err != nil {
			return err
		}
		proofVerification := VerifyCommerceProof(proof, proof.Challenge)
		if !proofVerification.OK {
			return NewError(CodeIntegrityFailed, "commerce proof verification failed before submit", proofVerification)
		}
		if !proof.Payload.OK {
			return NewError(CodeIntegrityFailed, "local commerce integrity failed; refusing to submit proof for server receipt", map[string]any{"summary": proof.Payload.Summary, "ledgers": proof.Payload.Ledgers})
		}

		submittedAt := time.Now().UTC().Format(time.RFC3339)
		request := commerceProofSubmitRequest{
			SchemaVersion: 1,
			ClientVersion: Version,
			SubmittedAt:   submittedAt,
			Proof:         proof,
			Verification:  proofVerification,
		}
		var response commerceProofSubmitResponse
		if err := s.proJSON(ctx, http.MethodPost, "/v1/commerce/proofs", request, &response); err != nil {
			return err
		}
		receipt := response.Receipt
		verification := VerifyCommerceReceipt(proof, receipt)
		if !verification.OK {
			return NewError(CodeIntegrityFailed, "commerce receipt verification failed", verification)
		}
		trust, err := s.trustCommerceReceiptKey(receipt)
		if err != nil {
			return err
		}
		verification.Trust = &trust
		verification.TrustStatus = trust.Status
		verification.ServerKeyTrusted = trust.OK
		signed, err := s.appendCommerceReceipt(receipt)
		if err != nil {
			return err
		}
		signedVerification := VerifyCommerceReceipt(proof, signed)
		signedVerification.Trust = &trust
		signedVerification.TrustStatus = trust.Status
		signedVerification.ServerKeyTrusted = trust.OK
		signedVerification.OK = signedVerification.OK && trust.OK
		result = CommerceReceiptSubmitResult{
			SchemaVersion: 1,
			SubmittedAt:   submittedAt,
			Proof:         proof,
			Receipt:       signed,
			Verification:  signedVerification,
		}
		return nil
	})
	return result, err
}

func VerifyCommerceReceipt(proof CommerceProof, receipt CommerceReceipt) CommerceReceiptVerificationResult {
	now := time.Now().UTC().Format(time.RFC3339)
	result := CommerceReceiptVerificationResult{
		SchemaVersion:       1,
		VerifiedAt:          now,
		ExpectedPayloadHash: strings.TrimSpace(proof.PayloadHash),
		ActualPayloadHash:   strings.TrimSpace(receipt.ProofPayloadHash),
		ReceiptID:           strings.TrimSpace(receipt.ReceiptID),
		Status:              strings.TrimSpace(receipt.Status),
	}
	result.ReceiptMatched = receipt.SchemaVersion == 1 &&
		strings.TrimSpace(receipt.ReceiptID) != "" &&
		strings.TrimSpace(receipt.Status) != "" &&
		strings.TrimSpace(receipt.ReceivedAt) != "" &&
		strings.TrimSpace(receipt.Algorithm) == commerceReceiptAlgorithm &&
		strings.TrimSpace(receipt.KeyID) != "" &&
		strings.TrimSpace(receipt.PublicKey) != "" &&
		strings.TrimSpace(receipt.ServerSignature) != ""
	result.ProofMatched = strings.TrimSpace(proof.PayloadHash) != "" &&
		strings.TrimSpace(receipt.ProofPayloadHash) == strings.TrimSpace(proof.PayloadHash) &&
		strings.TrimSpace(receipt.ProofSignature) == strings.TrimSpace(proof.Signature) &&
		strings.TrimSpace(receipt.ProofKeyID) == strings.TrimSpace(proof.KeyID) &&
		strings.TrimSpace(receipt.Challenge) == strings.TrimSpace(proof.Challenge)
	if strings.TrimSpace(receipt.DeviceID) != "" && strings.TrimSpace(proof.Payload.DeviceID) != "" && strings.TrimSpace(receipt.DeviceID) != strings.TrimSpace(proof.Payload.DeviceID) {
		result.ProofMatched = false
	}
	result.ProofSignatureMatched = VerifyCommerceProof(proof, proof.Challenge).OK

	payloadBytes, err := commerceReceiptPayloadBytes(receipt)
	if err != nil {
		result.Reason = err.Error()
		return result
	}
	publicKey, err := base64.StdEncoding.DecodeString(strings.TrimSpace(receipt.PublicKey))
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		result.Reason = "invalid receipt public key"
		return result
	}
	signature, err := base64.StdEncoding.DecodeString(strings.TrimSpace(receipt.ServerSignature))
	if err != nil {
		result.Reason = "invalid receipt signature"
		return result
	}
	result.ServerSignatureMatched = ed25519.Verify(ed25519.PublicKey(publicKey), payloadBytes, signature)
	result.ServerKeyTrusted = false
	result.OK = result.ReceiptMatched && result.ProofMatched && result.ProofSignatureMatched && result.ServerSignatureMatched
	if !result.OK && strings.TrimSpace(result.Reason) == "" {
		result.Reason = "commerce receipt verification failed"
	}
	return result
}

func (s *Service) ListCommerceReceipts(options RecordQueryOptions) (CommerceReceiptListResult, error) {
	if err := ValidateRecordQueryOptions(options); err != nil {
		return CommerceReceiptListResult{}, err
	}
	records, integrity, err := s.readCommerceReceiptsWithIntegrity()
	if err != nil {
		return CommerceReceiptListResult{}, err
	}
	filtered := make([]CommerceReceipt, 0, len(records))
	for _, record := range records {
		if options.Status != "" && normalizeName(record.Status) != normalizeName(options.Status) {
			continue
		}
		if !recordTimeInRange(record.ReceivedAt, options) {
			continue
		}
		filtered = append(filtered, record)
	}
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].ReceivedAt > filtered[j].ReceivedAt
	})
	if options.Limit > 0 && len(filtered) > options.Limit {
		filtered = filtered[:options.Limit]
	}
	trust, err := s.CommerceReceiptTrust()
	if err != nil {
		return CommerceReceiptListResult{}, err
	}
	return CommerceReceiptListResult{Records: filtered, Integrity: &integrity, Trust: &trust}, nil
}

func (s *Service) CommerceReceiptIntegrity() (LedgerIntegritySummary, error) {
	summary, _, err := s.verifyCommerceReceiptsFromDisk()
	return summary, err
}

func (s *Service) CommerceReceiptTrust() (CommerceReceiptTrustResult, error) {
	if err := s.ensureCommerceLedgerPrivatePaths(); err != nil {
		return CommerceReceiptTrustResult{}, err
	}
	state, err := s.loadCommerceReceiptTrustState()
	if err != nil {
		return CommerceReceiptTrustResult{
			SchemaVersion: 1,
			OK:            false,
			Status:        commerceReceiptTrustFailed,
			Reason:        err.Error(),
		}, nil
	}
	if state == nil {
		return CommerceReceiptTrustResult{
			SchemaVersion: 1,
			OK:            true,
			Status:        commerceReceiptTrustUnbound,
		}, nil
	}
	return trustResultFromState(*state), nil
}

func (s *Service) appendCommerceReceipt(record CommerceReceipt) (CommerceReceipt, error) {
	return s.appendSignedCommerceReceipt(record)
}

func (s *Service) appendSignedCommerceReceipt(record CommerceReceipt) (CommerceReceipt, error) {
	if err := s.ensureCommerceLedgerPrivatePaths(); err != nil {
		return CommerceReceipt{}, err
	}
	signed, err := s.signCommerceReceipt(record)
	if err != nil {
		return CommerceReceipt{}, err
	}
	if err := appendJSONLine(s.commerceReceiptsPath(), signed); err != nil {
		return CommerceReceipt{}, err
	}
	if err := s.writeLedgerHead(commerceReceiptsFile, signed.Integrity); err != nil {
		return CommerceReceipt{}, err
	}
	return signed, nil
}

func (s *Service) signCommerceReceipt(record CommerceReceipt) (CommerceReceipt, error) {
	key, err := s.loadOrCreateLedgerIntegrityKey()
	if err != nil {
		return CommerceReceipt{}, err
	}
	summary, err := s.verifyCommerceReceiptLedger()
	if err != nil {
		return CommerceReceipt{}, err
	}
	if summary.Failed > 0 {
		return CommerceReceipt{}, NewError(CodeIntegrityFailed, "commerce receipt ledger integrity failed; refusing to append trusted record", summary)
	}
	record.Integrity = nil
	hash, err := recordIntegrityHash(key, commerceReceiptsFile, summary.LastHash, record)
	if err != nil {
		return CommerceReceipt{}, err
	}
	record.Integrity = &RecordIntegrity{
		Algorithm:    ledgerIntegrityAlgorithm,
		Ledger:       commerceReceiptsFile,
		KeyID:        key.KeyID,
		Sequence:     summary.Records + 1,
		PreviousHash: summary.LastHash,
		Hash:         hash,
		SignedAt:     time.Now().UTC().Format(time.RFC3339),
		Status:       integrityStatusVerified,
		HeadHash:     hash,
		HeadMatched:  true,
	}
	return record, nil
}

func (s *Service) readCommerceReceiptsWithIntegrity() ([]CommerceReceipt, LedgerIntegritySummary, error) {
	var records []CommerceReceipt
	if err := readJSONLines(s.commerceReceiptsPath(), &records); err != nil {
		return nil, LedgerIntegritySummary{}, err
	}
	summary, records, err := s.verifyCommerceReceipts(records)
	if err != nil {
		return nil, LedgerIntegritySummary{}, err
	}
	return records, summary, nil
}

func (s *Service) verifyCommerceReceiptLedger() (LedgerIntegritySummary, error) {
	var records []CommerceReceipt
	if err := readJSONLines(s.commerceReceiptsPath(), &records); err != nil {
		return LedgerIntegritySummary{}, err
	}
	summary, _, err := s.verifyCommerceReceipts(records)
	return summary, err
}

func (s *Service) verifyCommerceReceiptsFromDisk() (LedgerIntegritySummary, []CommerceReceipt, error) {
	records, summary, err := s.readCommerceReceiptsWithIntegrity()
	return summary, records, err
}

func (s *Service) verifyCommerceReceipts(records []CommerceReceipt) (LedgerIntegritySummary, []CommerceReceipt, error) {
	key, err := s.loadLedgerIntegrityKeyIfPresent()
	if err != nil {
		return LedgerIntegritySummary{}, nil, err
	}
	head, err := s.readLedgerHead(commerceReceiptsFile)
	if err != nil {
		return LedgerIntegritySummary{}, nil, err
	}
	anchors := s.readLedgerAnchors(commerceReceiptsFile, key)
	summary := newLedgerIntegritySummary(commerceReceiptsFile, key, head, len(records))
	lastHash := ""
	for index := range records {
		record := records[index]
		result := verifyRecordIntegrity(key, commerceReceiptsFile, head, index+1, lastHash, record, record.Integrity)
		records[index].Integrity = result
		applyIntegrityResult(&summary, result)
		if result.Hash != "" {
			lastHash = result.Hash
		}
	}
	return finalizeLedgerIntegritySummary(summary, lastHash, head, anchors), records, nil
}

func (s *Service) trustCommerceReceiptKey(receipt CommerceReceipt) (CommerceReceiptTrustResult, error) {
	if err := s.ensureCommerceLedgerPrivatePaths(); err != nil {
		return CommerceReceiptTrustResult{}, err
	}
	candidate, err := commerceReceiptTrustStateFromReceipt(receipt)
	if err != nil {
		result := CommerceReceiptTrustResult{
			SchemaVersion: 1,
			OK:            false,
			Status:        commerceReceiptTrustFailed,
			Reason:        err.Error(),
		}
		return result, NewError(CodeIntegrityFailed, "commerce receipt trust check failed", result)
	}
	existing, err := s.loadCommerceReceiptTrustState()
	if err != nil {
		result := trustResultFromState(candidate)
		result.OK = false
		result.Status = commerceReceiptTrustFailed
		result.Reason = err.Error()
		return result, NewError(CodeIntegrityFailed, "commerce receipt trust check failed", result)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if existing == nil {
		candidate.BoundAt = now
		candidate.LastSeenAt = now
		if err := s.writeCommerceReceiptTrustState(candidate); err != nil {
			return CommerceReceiptTrustResult{}, err
		}
		return trustResultFromState(candidate), nil
	}
	if existing.Issuer != candidate.Issuer || existing.KeyID != candidate.KeyID || existing.PublicKey != candidate.PublicKey || existing.ReceiptAlgorithm != candidate.ReceiptAlgorithm {
		trusted := trustResultFromState(*existing)
		trusted.OK = false
		trusted.Status = commerceReceiptTrustFailed
		trusted.Reason = "commerce receipt server signing key changed"
		return trusted, NewError(CodeIntegrityFailed, "commerce receipt server signing key changed; refusing to append trusted receipt", map[string]any{
			"trusted":  trusted,
			"received": trustResultFromState(candidate),
		})
	}
	existing.LastSeenAt = now
	if err := s.writeCommerceReceiptTrustState(*existing); err != nil {
		return CommerceReceiptTrustResult{}, err
	}
	return trustResultFromState(*existing), nil
}

func (s *Service) loadCommerceReceiptTrustState() (*commerceReceiptTrustState, error) {
	data, err := readFileLimited(s.commerceReceiptTrustPath(), defaultRecordMaxBytes, "commerce receipt trust")
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var state commerceReceiptTrustState
	if err := decodeJSONStrict(data, &state); err != nil {
		return nil, NewError(CodeIntegrityFailed, "invalid commerce receipt trust file", map[string]any{"path": s.commerceReceiptTrustPath(), "error": err.Error()})
	}
	if err := s.verifyCommerceReceiptTrustState(state); err != nil {
		return nil, err
	}
	return &state, nil
}

func (s *Service) verifyCommerceReceiptTrustState(state commerceReceiptTrustState) error {
	if state.SchemaVersion != 1 || state.Status != commerceReceiptTrustBound || state.Algorithm != commerceReceiptTrustAlgorithm || strings.TrimSpace(state.KeyID) == "" || strings.TrimSpace(state.PublicKey) == "" || strings.TrimSpace(state.ReceiptAlgorithm) == "" || strings.TrimSpace(state.Hash) == "" {
		return NewError(CodeIntegrityFailed, "invalid commerce receipt trust state", map[string]any{"path": s.commerceReceiptTrustPath()})
	}
	key, err := s.loadLedgerIntegrityKeyIfPresent()
	if err != nil {
		return err
	}
	if key == nil {
		return NewError(CodeIntegrityFailed, "commerce receipt trust exists but local integrity key is missing", map[string]any{"path": s.commerceReceiptTrustPath()})
	}
	if state.LocalKeyID != key.KeyID {
		return NewError(CodeIntegrityFailed, "commerce receipt trust local key mismatch", map[string]any{"path": s.commerceReceiptTrustPath()})
	}
	expected, err := commerceReceiptTrustHash(*key, state)
	if err != nil {
		return err
	}
	if !hmac.Equal([]byte(expected), []byte(state.Hash)) {
		return NewError(CodeIntegrityFailed, "commerce receipt trust hash mismatch", map[string]any{"path": s.commerceReceiptTrustPath()})
	}
	return nil
}

func (s *Service) writeCommerceReceiptTrustState(state commerceReceiptTrustState) error {
	key, err := s.loadOrCreateLedgerIntegrityKey()
	if err != nil {
		return err
	}
	state.SchemaVersion = 1
	state.Status = commerceReceiptTrustBound
	state.Algorithm = commerceReceiptTrustAlgorithm
	state.LocalKeyID = key.KeyID
	hash, err := commerceReceiptTrustHash(key, state)
	if err != nil {
		return err
	}
	state.Hash = hash
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(s.commerceReceiptTrustPath(), append(data, '\n'), ledgerPrivateFileMode)
}

func commerceReceiptTrustHash(key ledgerIntegrityKey, state commerceReceiptTrustState) (string, error) {
	state.Hash = ""
	data, err := json.Marshal(state)
	if err != nil {
		return "", err
	}
	secret, err := hex.DecodeString(strings.TrimSpace(key.Secret))
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte("agtx-commerce-receipt-trust-v1"))
	mac.Write(data)
	return hex.EncodeToString(mac.Sum(nil)), nil
}

func commerceReceiptTrustStateFromReceipt(receipt CommerceReceipt) (commerceReceiptTrustState, error) {
	if strings.TrimSpace(receipt.KeyID) == "" || strings.TrimSpace(receipt.PublicKey) == "" || strings.TrimSpace(receipt.Algorithm) != commerceReceiptAlgorithm {
		return commerceReceiptTrustState{}, NewError(CodeIntegrityFailed, "receipt is missing server signing key fields", nil)
	}
	publicKey, err := base64.StdEncoding.DecodeString(strings.TrimSpace(receipt.PublicKey))
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		return commerceReceiptTrustState{}, NewError(CodeIntegrityFailed, "receipt has invalid server public key", nil)
	}
	return commerceReceiptTrustState{
		SchemaVersion:    1,
		Status:           commerceReceiptTrustBound,
		Issuer:           strings.TrimSpace(receipt.Issuer),
		KeyID:            strings.TrimSpace(receipt.KeyID),
		PublicKey:        strings.TrimSpace(receipt.PublicKey),
		ReceiptAlgorithm: strings.TrimSpace(receipt.Algorithm),
	}, nil
}

func trustResultFromState(state commerceReceiptTrustState) CommerceReceiptTrustResult {
	return CommerceReceiptTrustResult{
		SchemaVersion:    1,
		OK:               state.Status == commerceReceiptTrustBound,
		Status:           state.Status,
		Issuer:           state.Issuer,
		KeyID:            state.KeyID,
		PublicKey:        state.PublicKey,
		ReceiptAlgorithm: state.ReceiptAlgorithm,
		BoundAt:          state.BoundAt,
		LastSeenAt:       state.LastSeenAt,
	}
}

func commerceReceiptPayloadBytes(receipt CommerceReceipt) ([]byte, error) {
	receipt.ServerSignature = ""
	receipt.Integrity = nil
	data, err := json.Marshal(receipt)
	if err != nil {
		return nil, err
	}
	var normalized any
	if err := json.Unmarshal(data, &normalized); err != nil {
		return nil, err
	}
	return json.Marshal(normalized)
}

func commerceReceiptIDForProof(proof CommerceProof) string {
	hash := sha256.Sum256([]byte(strings.TrimSpace(proof.PayloadHash) + "\n" + strings.TrimSpace(proof.Signature)))
	return "receipt-" + hex.EncodeToString(hash[:12])
}

func (s *Service) commerceReceiptsPath() string {
	return filepath.Join(s.Paths.ConfigDir, commerceReceiptsFile)
}

func (s *Service) commerceReceiptTrustPath() string {
	return filepath.Join(s.Paths.ConfigDir, commerceReceiptTrustFile)
}
