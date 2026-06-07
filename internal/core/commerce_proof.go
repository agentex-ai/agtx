package core

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	commerceProofAlgorithm        = "ed25519-commerce-proof-v1"
	commerceProofKeyFile          = "commerce-proof-key.json"
	commerceProofSubject          = "agtx.local.commerce_ledgers"
	commerceProofTrustLevel       = "local_signed"
	commerceProofReceiptLocalOnly = "local_only"
	commerceProofChallengeMax     = 512
)

type commerceProofKey struct {
	SchemaVersion int    `json:"schema_version"`
	Algorithm     string `json:"algorithm"`
	KeyID         string `json:"key_id"`
	PublicKey     string `json:"public_key"`
	PrivateKey    string `json:"private_key"`
	CreatedAt     string `json:"created_at"`
}

type commerceProofSigningKey struct {
	KeyID      string
	PublicKey  ed25519.PublicKey
	PrivateKey ed25519.PrivateKey
}

func (s *Service) CommerceProof(challenge string) (CommerceProof, error) {
	challenge, err := normalizeCommerceProofChallenge(challenge)
	if err != nil {
		return CommerceProof{}, err
	}
	key, err := s.loadOrCreateCommerceProofKey()
	if err != nil {
		return CommerceProof{}, err
	}
	integrity, err := s.CommerceIntegrity()
	if err != nil {
		return CommerceProof{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	publicKey := base64.StdEncoding.EncodeToString(key.PublicKey)
	payload := CommerceProofPayload{
		SchemaVersion: 1,
		GeneratedAt:   now,
		Challenge:     challenge,
		Subject:       commerceProofSubject,
		TrustLevel:    commerceProofTrustLevel,
		ReceiptStatus: commerceProofReceiptLocalOnly,
		Algorithm:     commerceProofAlgorithm,
		KeyID:         key.KeyID,
		PublicKey:     publicKey,
		DeviceID:      strings.TrimSpace(s.Auth.DeviceID),
		OK:            integrity.OK,
		Summary:       integrity.Summary,
		Ledgers:       integrity.Ledgers,
		Checks:        integrity.Checks,
	}
	payloadBytes, err := commerceProofPayloadBytes(payload)
	if err != nil {
		return CommerceProof{}, err
	}
	hash := sha256.Sum256(payloadBytes)
	signature := ed25519.Sign(key.PrivateKey, payloadBytes)
	return CommerceProof{
		SchemaVersion: 1,
		GeneratedAt:   now,
		Challenge:     challenge,
		Subject:       commerceProofSubject,
		TrustLevel:    commerceProofTrustLevel,
		ReceiptStatus: commerceProofReceiptLocalOnly,
		Algorithm:     commerceProofAlgorithm,
		KeyID:         key.KeyID,
		PublicKey:     publicKey,
		PayloadHash:   hex.EncodeToString(hash[:]),
		Signature:     base64.StdEncoding.EncodeToString(signature),
		Payload:       payload,
	}, nil
}

func VerifyCommerceProof(proof CommerceProof, expectedChallenge string) CommerceProofVerificationResult {
	expectedChallenge = strings.TrimSpace(expectedChallenge)
	result := CommerceProofVerificationResult{
		SchemaVersion:     1,
		VerifiedAt:        time.Now().UTC().Format(time.RFC3339),
		ExpectedChallenge: expectedChallenge,
		ActualChallenge:   proof.Payload.Challenge,
		KeyID:             strings.TrimSpace(proof.KeyID),
		PayloadHash:       strings.TrimSpace(proof.PayloadHash),
	}
	result.AlgorithmMatched = proof.Algorithm == commerceProofAlgorithm
	result.ChallengeMatched = expectedChallenge != "" && proof.Payload.Challenge == expectedChallenge && proof.Challenge == proof.Payload.Challenge
	result.EnvelopeMatched = proof.SchemaVersion == 1 &&
		proof.GeneratedAt == proof.Payload.GeneratedAt &&
		proof.Subject == proof.Payload.Subject &&
		proof.TrustLevel == proof.Payload.TrustLevel &&
		proof.ReceiptStatus == proof.Payload.ReceiptStatus &&
		proof.Algorithm == proof.Payload.Algorithm &&
		proof.KeyID == proof.Payload.KeyID &&
		proof.PublicKey == proof.Payload.PublicKey &&
		proof.Subject == commerceProofSubject &&
		proof.TrustLevel == commerceProofTrustLevel &&
		proof.ReceiptStatus == commerceProofReceiptLocalOnly &&
		proof.Payload.Algorithm == commerceProofAlgorithm

	payloadBytes, err := commerceProofPayloadBytes(proof.Payload)
	if err != nil {
		result.Reason = err.Error()
		return result
	}
	hash := sha256.Sum256(payloadBytes)
	result.CalculatedHash = hex.EncodeToString(hash[:])
	result.PayloadHashMatched = result.PayloadHash != "" && result.PayloadHash == result.CalculatedHash

	publicKey, err := base64.StdEncoding.DecodeString(strings.TrimSpace(proof.PublicKey))
	if err != nil {
		result.Reason = "invalid public key"
		return result
	}
	signature, err := base64.StdEncoding.DecodeString(strings.TrimSpace(proof.Signature))
	if err != nil {
		result.Reason = "invalid signature"
		return result
	}
	if len(publicKey) != ed25519.PublicKeySize {
		result.Reason = "invalid public key size"
		return result
	}
	result.SignatureMatched = ed25519.Verify(ed25519.PublicKey(publicKey), payloadBytes, signature)
	result.OK = result.AlgorithmMatched && result.ChallengeMatched && result.PayloadHashMatched && result.SignatureMatched && result.EnvelopeMatched
	if !result.OK && strings.TrimSpace(result.Reason) == "" {
		result.Reason = "commerce proof verification failed"
	}
	return result
}

func normalizeCommerceProofChallenge(challenge string) (string, error) {
	challenge = strings.TrimSpace(challenge)
	if challenge == "" {
		return "", NewError(CodeInvalidArgument, "commerce proof challenge is required", map[string]any{"field": "challenge", "max_bytes": commerceProofChallengeMax})
	}
	if strings.ContainsRune(challenge, '\x00') {
		return "", NewError(CodeInvalidArgument, "commerce proof challenge contains NUL byte", map[string]any{"field": "challenge"})
	}
	if len([]byte(challenge)) > commerceProofChallengeMax {
		return "", NewError(CodeInvalidArgument, "commerce proof challenge exceeds size limit", map[string]any{"field": "challenge", "max_bytes": commerceProofChallengeMax})
	}
	return challenge, nil
}

func (s *Service) loadOrCreateCommerceProofKey() (commerceProofSigningKey, error) {
	if err := s.ensureCommerceLedgerPrivatePaths(); err != nil {
		return commerceProofSigningKey{}, err
	}
	if key, err := s.loadCommerceProofKeyIfPresent(); err != nil {
		return commerceProofSigningKey{}, err
	} else if key != nil {
		return *key, nil
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return commerceProofSigningKey{}, err
	}
	key := commerceProofKey{
		SchemaVersion: 1,
		Algorithm:     commerceProofAlgorithm,
		KeyID:         "commerce-proof-" + NewTraceID(),
		PublicKey:     base64.StdEncoding.EncodeToString(publicKey),
		PrivateKey:    base64.StdEncoding.EncodeToString(privateKey),
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.MarshalIndent(key, "", "  ")
	if err != nil {
		return commerceProofSigningKey{}, err
	}
	if err := writeFileAtomic(s.commerceProofKeyPath(), append(data, '\n'), ledgerPrivateFileMode); err != nil {
		return commerceProofSigningKey{}, err
	}
	return commerceProofSigningKey{KeyID: key.KeyID, PublicKey: publicKey, PrivateKey: privateKey}, nil
}

func (s *Service) loadCommerceProofKeyIfPresent() (*commerceProofSigningKey, error) {
	data, err := readFileLimited(s.commerceProofKeyPath(), defaultRecordMaxBytes, "commerce proof key")
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var key commerceProofKey
	if err := decodeJSONStrict(data, &key); err != nil {
		return nil, NewError(CodeInvalidArgument, "invalid commerce proof key", map[string]any{"path": s.commerceProofKeyPath(), "error": err.Error()})
	}
	if key.SchemaVersion != 1 || key.Algorithm != commerceProofAlgorithm || strings.TrimSpace(key.KeyID) == "" {
		return nil, NewError(CodeIntegrityFailed, "invalid commerce proof key", map[string]any{"path": s.commerceProofKeyPath()})
	}
	publicKey, err := base64.StdEncoding.DecodeString(strings.TrimSpace(key.PublicKey))
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		return nil, NewError(CodeIntegrityFailed, "invalid commerce proof public key", map[string]any{"path": s.commerceProofKeyPath()})
	}
	privateKey, err := base64.StdEncoding.DecodeString(strings.TrimSpace(key.PrivateKey))
	if err != nil || len(privateKey) != ed25519.PrivateKeySize {
		return nil, NewError(CodeIntegrityFailed, "invalid commerce proof private key", map[string]any{"path": s.commerceProofKeyPath()})
	}
	derivedPublic := ed25519.PrivateKey(privateKey).Public().(ed25519.PublicKey)
	if !bytes.Equal(derivedPublic, publicKey) {
		return nil, NewError(CodeIntegrityFailed, "commerce proof key pair mismatch", map[string]any{"path": s.commerceProofKeyPath()})
	}
	return &commerceProofSigningKey{KeyID: key.KeyID, PublicKey: ed25519.PublicKey(publicKey), PrivateKey: ed25519.PrivateKey(privateKey)}, nil
}

func commerceProofPayloadBytes(payload CommerceProofPayload) ([]byte, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	var normalized any
	if err := json.Unmarshal(data, &normalized); err != nil {
		return nil, err
	}
	return json.Marshal(normalized)
}

func (s *Service) commerceProofKeyPath() string {
	return filepath.Join(s.Paths.ConfigDir, commerceProofKeyFile)
}
