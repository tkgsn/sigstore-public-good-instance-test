// Command verify_rekor_v2_bundle independently verifies the Rekor v2 Merkle
// inclusion proof and the signed C2SP checkpoint contained in a Sigstore bundle.
// It uses only the Go standard library.
package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type keyID struct {
	KeyID string `json:"keyId"`
}

type inclusionProof struct {
	LogIndex   string   `json:"logIndex"`
	RootHash   string   `json:"rootHash"`
	TreeSize   string   `json:"treeSize"`
	Hashes     []string `json:"hashes"`
	Checkpoint struct {
		Envelope string `json:"envelope"`
	} `json:"checkpoint"`
}

type tlogEntry struct {
	LogIndex          string          `json:"logIndex"`
	LogID             keyID           `json:"logId"`
	CanonicalizedBody string          `json:"canonicalizedBody"`
	InclusionProof    *inclusionProof `json:"inclusionProof"`
}

type bundleDocument struct {
	VerificationMaterial struct {
		TlogEntries []tlogEntry `json:"tlogEntries"`
	} `json:"verificationMaterial"`
}

type trustedLog struct {
	BaseURL         string `json:"baseUrl"`
	LogID           keyID  `json:"logId"`
	CheckpointKeyID keyID  `json:"checkpointKeyId"`
	PublicKey       struct {
		RawBytes   string `json:"rawBytes"`
		KeyDetails string `json:"keyDetails"`
	} `json:"publicKey"`
}

type trustedRootDocument struct {
	Tlogs []trustedLog `json:"tlogs"`
}

func main() {
	bundlePath := flag.String("bundle", "", "path to a Sigstore bundle")
	trustedRootPath := flag.String("trusted-root", "", "path to a Sigstore TrustedRoot JSON file")
	flag.Parse()

	if *bundlePath == "" || *trustedRootPath == "" {
		flag.Usage()
		os.Exit(2)
	}

	if err := verify(*bundlePath, *trustedRootPath); err != nil {
		fmt.Fprintln(os.Stderr, "verification failed:", err)
		os.Exit(1)
	}
}

func verify(bundlePath, trustedRootPath string) error {
	var bundle bundleDocument
	if err := readJSON(bundlePath, &bundle); err != nil {
		return err
	}
	if len(bundle.VerificationMaterial.TlogEntries) != 1 {
		return fmt.Errorf("expected exactly one tlog entry, got %d", len(bundle.VerificationMaterial.TlogEntries))
	}

	var trustedRoot trustedRootDocument
	if err := readJSON(trustedRootPath, &trustedRoot); err != nil {
		return err
	}

	entry := bundle.VerificationMaterial.TlogEntries[0]
	if entry.InclusionProof == nil {
		return errors.New("bundle has no inclusion proof")
	}
	proof := entry.InclusionProof

	logIndex, err := strconv.ParseUint(entry.LogIndex, 10, 64)
	if err != nil {
		return fmt.Errorf("parse log index: %w", err)
	}
	proofIndex, err := strconv.ParseUint(proof.LogIndex, 10, 64)
	if err != nil {
		return fmt.Errorf("parse proof log index: %w", err)
	}
	if logIndex != proofIndex {
		return fmt.Errorf("entry log index %d differs from proof index %d", logIndex, proofIndex)
	}

	checkpoint, err := parseCheckpoint(proof.Checkpoint.Envelope)
	if err != nil {
		return err
	}
	if checkpoint.TreeSize == 0 || logIndex >= checkpoint.TreeSize {
		return fmt.Errorf("log index %d is outside checkpoint tree size %d", logIndex, checkpoint.TreeSize)
	}

	proofTreeSize, err := strconv.ParseUint(proof.TreeSize, 10, 64)
	if err != nil {
		return fmt.Errorf("parse proof tree size: %w", err)
	}
	if proofTreeSize != checkpoint.TreeSize {
		return fmt.Errorf("proof tree size %d differs from signed checkpoint tree size %d", proofTreeSize, checkpoint.TreeSize)
	}

	proofRoot, err := decodeBase64("proof root hash", proof.RootHash)
	if err != nil {
		return err
	}
	if !bytes.Equal(proofRoot, checkpoint.RootHash) {
		return errors.New("proof root hash differs from signed checkpoint root hash")
	}

	body, err := decodeBase64("canonicalized body", entry.CanonicalizedBody)
	if err != nil {
		return err
	}
	var canonicalEntry struct {
		Kind       string `json:"kind"`
		APIVersion string `json:"apiVersion"`
	}
	if err := json.Unmarshal(body, &canonicalEntry); err != nil {
		return fmt.Errorf("parse canonicalized body: %w", err)
	}
	if canonicalEntry.Kind != "hashedrekord" || canonicalEntry.APIVersion != "0.0.2" {
		return fmt.Errorf("expected Rekor v2 hashedrekord 0.0.2, got %s %s", canonicalEntry.Kind, canonicalEntry.APIVersion)
	}
	computedRoot, err := computeRoot(body, logIndex, checkpoint.TreeSize, proof.Hashes)
	if err != nil {
		return err
	}
	if !bytes.Equal(computedRoot, checkpoint.RootHash) {
		return fmt.Errorf("computed Merkle root %s differs from checkpoint root %s",
			base64.StdEncoding.EncodeToString(computedRoot),
			base64.StdEncoding.EncodeToString(checkpoint.RootHash))
	}

	trustedLog, err := findTrustedLog(trustedRoot.Tlogs, entry.LogID.KeyID)
	if err != nil {
		return err
	}
	if checkpoint.Origin != strings.TrimPrefix(trustedLog.BaseURL, "https://") {
		return fmt.Errorf("checkpoint origin %q does not match trusted log URL %q", checkpoint.Origin, trustedLog.BaseURL)
	}
	if err := verifyCheckpointSignature(checkpoint, trustedLog, entry.LogID.KeyID); err != nil {
		return err
	}

	fmt.Println("Rekor v2 verification succeeded")
	fmt.Println("  log URL:            ", trustedLog.BaseURL)
	fmt.Println("  entry type:         ", canonicalEntry.Kind, canonicalEntry.APIVersion)
	fmt.Println("  log index:          ", logIndex)
	fmt.Println("  checkpoint tree size:", checkpoint.TreeSize)
	fmt.Println("  audit path hashes:  ", len(proof.Hashes))
	fmt.Println("  root hash (base64): ", base64.StdEncoding.EncodeToString(computedRoot))
	fmt.Println("  inclusion proof:     valid")
	fmt.Println("  checkpoint signature: valid")
	return nil
}

type checkpointData struct {
	Origin         string
	TreeSize       uint64
	RootHash       []byte
	SignedText     []byte
	SignatureLines []string
}

func parseCheckpoint(envelope string) (checkpointData, error) {
	separator := "\n— "
	separatorIndex := strings.Index(envelope, separator)
	if separatorIndex < 0 {
		return checkpointData{}, errors.New("checkpoint contains no signature lines")
	}

	signedText := envelope[:separatorIndex]
	lines := strings.Split(strings.TrimSuffix(signedText, "\n"), "\n")
	if len(lines) < 3 {
		return checkpointData{}, errors.New("checkpoint has fewer than three body lines")
	}
	treeSize, err := strconv.ParseUint(lines[1], 10, 64)
	if err != nil {
		return checkpointData{}, fmt.Errorf("parse checkpoint tree size: %w", err)
	}
	rootHash, err := decodeBase64("checkpoint root hash", lines[2])
	if err != nil {
		return checkpointData{}, err
	}

	signatureBlock := envelope[separatorIndex+1:]
	var signatureLines []string
	for _, line := range strings.Split(signatureBlock, "\n") {
		if strings.HasPrefix(line, "— ") {
			signatureLines = append(signatureLines, line)
		}
	}
	return checkpointData{
		Origin:         lines[0],
		TreeSize:       treeSize,
		RootHash:       rootHash,
		SignedText:     []byte(signedText),
		SignatureLines: signatureLines,
	}, nil
}

func computeRoot(canonicalizedBody []byte, logIndex, treeSize uint64, encodedHashes []string) ([]byte, error) {
	leafInput := append([]byte{0}, canonicalizedBody...)
	leafHash := sha256.Sum256(leafInput)
	computed := leafHash[:]
	fn := logIndex
	sn := treeSize - 1

	for position, encodedHash := range encodedHashes {
		if sn == 0 {
			return nil, errors.New("audit path is too long for the checkpoint tree size")
		}
		sibling, err := decodeBase64(fmt.Sprintf("audit path hash %d", position), encodedHash)
		if err != nil {
			return nil, err
		}
		if len(sibling) != sha256.Size {
			return nil, fmt.Errorf("audit path hash %d has length %d", position, len(sibling))
		}

		if fn&1 == 1 || fn == sn {
			computed = nodeHash(sibling, computed)
			for fn != 0 && fn&1 == 0 {
				fn >>= 1
				sn >>= 1
			}
		} else {
			computed = nodeHash(computed, sibling)
		}
		fn >>= 1
		sn >>= 1
	}
	if sn != 0 {
		return nil, errors.New("audit path is too short for the checkpoint tree size")
	}
	return computed, nil
}

func nodeHash(left, right []byte) []byte {
	input := make([]byte, 1, 1+len(left)+len(right))
	input[0] = 1
	input = append(input, left...)
	input = append(input, right...)
	hash := sha256.Sum256(input)
	return hash[:]
}

func findTrustedLog(logs []trustedLog, wantedID string) (trustedLog, error) {
	for _, log := range logs {
		if log.LogID.KeyID == wantedID || log.CheckpointKeyID.KeyID == wantedID {
			return log, nil
		}
	}
	return trustedLog{}, fmt.Errorf("log ID %q not found in TrustedRoot", wantedID)
}

func verifyCheckpointSignature(checkpoint checkpointData, log trustedLog, encodedLogID string) error {
	if log.PublicKey.KeyDetails != "PKIX_ED25519" {
		return fmt.Errorf("unsupported checkpoint key type %q", log.PublicKey.KeyDetails)
	}
	der, err := decodeBase64("checkpoint public key", log.PublicKey.RawBytes)
	if err != nil {
		return err
	}
	parsedKey, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return fmt.Errorf("parse checkpoint public key: %w", err)
	}
	publicKey, ok := parsedKey.(ed25519.PublicKey)
	if !ok {
		return errors.New("checkpoint public key is not Ed25519")
	}

	logID, err := decodeBase64("log ID", encodedLogID)
	if err != nil {
		return err
	}
	if len(logID) < 4 {
		return errors.New("log ID is shorter than the four-byte key hint")
	}

	for _, line := range checkpoint.SignatureLines {
		fields := strings.Fields(line)
		if len(fields) != 3 || fields[0] != "—" || fields[1] != checkpoint.Origin {
			continue
		}
		rawSignature, err := decodeBase64("checkpoint signature", fields[2])
		if err != nil || len(rawSignature) != 4+ed25519.SignatureSize {
			continue
		}
		if !bytes.Equal(rawSignature[:4], logID[:4]) {
			continue
		}
		if ed25519.Verify(publicKey, checkpoint.SignedText, rawSignature[4:]) {
			return nil
		}
	}
	return errors.New("no valid checkpoint signature matched the TrustedRoot key")
}

func readJSON(path string, destination any) error {
	contents, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(contents, destination); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}

func decodeBase64(label, encoded string) ([]byte, error) {
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", label, err)
	}
	return decoded, nil
}
