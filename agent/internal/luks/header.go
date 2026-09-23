// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package luks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

// BorEscrowTokenType is the LUKS2 token type of Bor's unassigned metadata
// token. It is never assigned to a keyslot (an assigned foreign token makes
// systemd-cryptenroll report the slot as "conflict"), so boot tooling
// ignores it; it lets a reinstalled agent recognise its slot and tells a
// human reading luksDump which slot Bor manages.
const BorEscrowTokenType = "bor-escrow"

// Header is the parsed LUKS2 JSON metadata of one volume.
type Header struct {
	// UUID is the volume UUID (supplied by discovery; not in the JSON area).
	UUID string
	// Keyslots by index, classified via the header tokens.
	Keyslots []*Keyslot
	// Cipher is the data segment encryption spec (e.g. aes-xts-plain64).
	Cipher string
	// VolumeKeyBits is the volume key size in bits (512 = AES-256-XTS).
	VolumeKeyBits int
	// JSONAreaFree estimates the metadata bytes left for new tokens.
	JSONAreaFree int
	// BorEscrow is the parsed bor-escrow token, when present.
	BorEscrow *BorEscrowToken
}

// Keyslot is one classified keyslot.
type Keyslot struct {
	Index       int
	Kind        pb.LuksKeyslotKind
	KDF         string // "pbkdf2-sha512", "argon2id", …
	Fingerprint string // hex sha256(uuid ":" index ":" kdf.salt)
	TPM2PCRs    []uint32
	// Clevis metadata decoded from the token's JWE protected header.
	ClevisPin              string
	ClevisThreshold        uint32
	TangURLs               []string
	TangSigningThumbprints []string
	// BorEscrowID is set on the slot the bor-escrow token points at.
	BorEscrowID string
	// TokenIDs are the header token ids bound to this slot.
	TokenIDs []string
}

// BorEscrowToken is Bor's unassigned marker token.
type BorEscrowToken struct {
	TokenID  string
	Version  int    `json:"v"`
	EscrowID string `json:"escrow_id"`
	Keyslot  string `json:"keyslot"`
	Server   string `json:"server"`
}

// luks2Metadata mirrors the parts of `cryptsetup luksDump --dump-json-metadata`
// output the agent reads (cryptsetup >= 2.4).
type luks2Metadata struct {
	Keyslots map[string]struct {
		Type    string `json:"type"`
		KeySize int    `json:"key_size"`
		KDF     struct {
			Type string `json:"type"`
			Hash string `json:"hash"`
			Salt string `json:"salt"`
		} `json:"kdf"`
	} `json:"keyslots"`
	Tokens   map[string]json.RawMessage `json:"tokens"`
	Segments map[string]struct {
		Encryption string `json:"encryption"`
	} `json:"segments"`
	Config struct {
		JSONSize string `json:"json_size"`
	} `json:"config"`
}

// headerToken is the common shape of a LUKS2 token.
type headerToken struct {
	Type     string   `json:"type"`
	Keyslots []string `json:"keyslots"`
	// systemd-tpm2:
	TPM2PCRs []uint32 `json:"tpm2-pcrs"`
	// clevis:
	JWE struct {
		Protected string `json:"protected"`
	} `json:"jwe"`
	// bor-escrow:
	Bor *BorEscrowToken `json:"bor"`
}

// DumpHeader reads and parses a device's LUKS2 header via
// `cryptsetup luksDump --dump-json-metadata` (works unprivileged on images,
// as root on block devices).
func DumpHeader(ctx context.Context, cryptsetup, device, uuid string) (*Header, error) {
	res, err := runCommand(ctx, &runRequest{
		Name: cryptsetup,
		Args: []string{"luksDump", "--dump-json-metadata", device},
	})
	if err != nil {
		return nil, fmt.Errorf("luks: dump header of %s: %w", device, err)
	}
	return ParseHeader([]byte(res.Stdout), uuid)
}

// ParseHeader parses LUKS2 JSON metadata and classifies the keyslots.
func ParseHeader(raw []byte, uuid string) (*Header, error) {
	var meta luks2Metadata
	if err := json.Unmarshal(raw, &meta); err != nil {
		return nil, fmt.Errorf("luks: parse header JSON: %w", err)
	}

	h := &Header{UUID: uuid}

	for _, seg := range meta.Segments {
		if seg.Encryption != "" {
			h.Cipher = seg.Encryption
			break
		}
	}

	// Token index: keyslot id -> tokens bound to it, plus the bor-escrow token.
	type slotTokenInfo struct {
		tokenID string
		token   *headerToken
	}
	slotTokens := map[string][]slotTokenInfo{}
	for tokenID, rawToken := range meta.Tokens {
		var t headerToken
		if err := json.Unmarshal(rawToken, &t); err != nil {
			continue // unknown token layout: never touched, not classified
		}
		if t.Type == BorEscrowTokenType && t.Bor != nil {
			t.Bor.TokenID = tokenID
			h.BorEscrow = t.Bor
			continue
		}
		for _, ks := range t.Keyslots {
			tCopy := t
			slotTokens[ks] = append(slotTokens[ks], slotTokenInfo{tokenID: tokenID, token: &tCopy})
		}
	}

	for idxStr, ks := range meta.Keyslots {
		idx, err := strconv.Atoi(idxStr)
		if err != nil {
			continue
		}
		if h.VolumeKeyBits == 0 && ks.KeySize > 0 {
			h.VolumeKeyBits = ks.KeySize * 8
		}
		slot := &Keyslot{
			Index:       idx,
			Kind:        pb.LuksKeyslotKind_LUKS_KEYSLOT_KIND_PASSWORD,
			KDF:         kdfLabel(ks.KDF.Type, ks.KDF.Hash),
			Fingerprint: KeyslotFingerprint(uuid, idx, ks.KDF.Salt),
		}
		for _, ti := range slotTokens[idxStr] {
			slot.TokenIDs = append(slot.TokenIDs, ti.tokenID)
			classifyToken(slot, ti.token)
		}
		if h.BorEscrow != nil && h.BorEscrow.Keyslot == idxStr &&
			slot.Kind == pb.LuksKeyslotKind_LUKS_KEYSLOT_KIND_RECOVERY {
			slot.Kind = pb.LuksKeyslotKind_LUKS_KEYSLOT_KIND_BOR_RECOVERY
			slot.BorEscrowID = h.BorEscrow.EscrowID
		}
		h.Keyslots = append(h.Keyslots, slot)
	}
	sort.Slice(h.Keyslots, func(i, j int) bool { return h.Keyslots[i].Index < h.Keyslots[j].Index })

	// Free JSON area ≈ configured json_size minus the current document.
	if size, err := strconv.Atoi(meta.Config.JSONSize); err == nil && size > len(raw) {
		h.JSONAreaFree = size - len(raw)
	}

	return h, nil
}

// classifyToken updates a slot's kind and metadata from one bound token.
// Later tokens never downgrade a more specific kind.
func classifyToken(slot *Keyslot, t *headerToken) {
	switch t.Type {
	case "systemd-tpm2":
		slot.Kind = pb.LuksKeyslotKind_LUKS_KEYSLOT_KIND_TPM2
		slot.TPM2PCRs = t.TPM2PCRs
	case "systemd-fido2":
		slot.Kind = pb.LuksKeyslotKind_LUKS_KEYSLOT_KIND_FIDO2
	case "systemd-pkcs11":
		slot.Kind = pb.LuksKeyslotKind_LUKS_KEYSLOT_KIND_PKCS11
	case "systemd-recovery":
		slot.Kind = pb.LuksKeyslotKind_LUKS_KEYSLOT_KIND_RECOVERY
	case "clevis":
		slot.Kind = pb.LuksKeyslotKind_LUKS_KEYSLOT_KIND_CLEVIS
		if info, err := DecodeClevisProtected(t.JWE.Protected); err == nil {
			slot.ClevisPin = info.Pin
			slot.ClevisThreshold = info.Threshold
			slot.TangURLs = info.TangURLs
			slot.TangSigningThumbprints = info.SigningThumbprints
		}
	default:
		if slot.Kind == pb.LuksKeyslotKind_LUKS_KEYSLOT_KIND_PASSWORD {
			slot.Kind = pb.LuksKeyslotKind_LUKS_KEYSLOT_KIND_OTHER
		}
	}
}

// kdfLabel builds the reported KDF string ("pbkdf2-sha512", "argon2id").
func kdfLabel(kdfType, hash string) string {
	if kdfType == "pbkdf2" && hash != "" {
		return "pbkdf2-" + hash
	}
	return kdfType
}

// KeyslotFingerprint is the stable identifier of a keyslot instance:
// hex sha256(luks_uuid ":" index ":" kdf.salt). The salt is random at slot
// creation, so re-keying or replacing the slot changes the fingerprint -
// drift detection without ever touching key material.
func KeyslotFingerprint(uuid string, index int, salt string) string {
	sum := sha256.Sum256([]byte(uuid + ":" + strconv.Itoa(index) + ":" + salt))
	return hex.EncodeToString(sum[:])
}

// ToProto converts the header to the wire inventory representation.
func (h *Header) ToProto() []*pb.LuksKeyslot {
	out := make([]*pb.LuksKeyslot, 0, len(h.Keyslots))
	for _, ks := range h.Keyslots {
		out = append(out, &pb.LuksKeyslot{
			Index:                  uint32(ks.Index), //nolint:gosec // LUKS2 slots are 0..31
			Kind:                   ks.Kind,
			Kdf:                    ks.KDF,
			Fingerprint:            ks.Fingerprint,
			Tpm2Pcrs:               ks.TPM2PCRs,
			ClevisPin:              ks.ClevisPin,
			ClevisThreshold:        ks.ClevisThreshold,
			TangUrls:               ks.TangURLs,
			TangSigningThumbprints: ks.TangSigningThumbprints,
			BorEscrowId:            ks.BorEscrowID,
		})
	}
	return out
}

// FindSlot returns the keyslot with the given index, or nil.
func (h *Header) FindSlot(index int) *Keyslot {
	for _, ks := range h.Keyslots {
		if ks.Index == index {
			return ks
		}
	}
	return nil
}

// SlotsOfKind returns the keyslots of one kind.
func (h *Header) SlotsOfKind(kind pb.LuksKeyslotKind) []*Keyslot {
	var out []*Keyslot
	for _, ks := range h.Keyslots {
		if ks.Kind == kind {
			out = append(out, ks)
		}
	}
	return out
}
