// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package export

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/VuteTech/Bor/server/internal/models"
	exportpb "github.com/VuteTech/Bor/server/pkg/grpc/export"
	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

// APIVersion is the only envelope version this build reads and writes.
const APIVersion = "bor.dev/v1"

// newContentMessage returns the concrete proto message for a policy type, or
// nil for types this build does not know. Import rejects unknown types: a
// policy that cannot round-trip the schema could not be edited or enforced.
func newContentMessage(policyType string) proto.Message {
	switch policyType {
	case "Firefox":
		return &pb.FirefoxPolicy{}
	case "Thunderbird":
		return &pb.ThunderbirdPolicy{}
	case "Chrome":
		return &pb.ChromePolicy{}
	case "Edge":
		return &pb.EdgePolicy{}
	case "Kconfig":
		return &pb.KConfigPolicy{}
	case "Dconf":
		return &pb.DConfPolicy{}
	case "Polkit":
		return &pb.PolkitPolicy{}
	case "Package":
		return &pb.PackagePolicy{}
	case "Firewalld":
		return &pb.FirewalldPolicy{}
	}
	return nil
}

// protoNameTypes are the types whose UI editors store original (snake_case)
// proto field names instead of the protojson default (json_name/camelCase).
// Canonical output must match what the editors produce, or imported policies
// would render differently from natively created ones.
var protoNameTypes = map[string]bool{
	"Polkit": true,
	"Dconf":  true,
}

// CanonicalizeContent round-trips a policy content document through its proto
// message: field-name casing is normalized to what the UI editors store,
// unknown fields are dropped, and output is deterministic (proto field order,
// 2-space indent) so exports diff cleanly in git.
func CanonicalizeContent(policyType, content string) (string, error) {
	msg := newContentMessage(policyType)
	if msg == nil {
		return "", fmt.Errorf("unknown policy type %q", policyType)
	}
	if strings.TrimSpace(content) == "" {
		content = "{}"
	}
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal([]byte(content), msg); err != nil {
		return "", fmt.Errorf("content does not match the %s schema: %w", policyType, err)
	}
	raw, err := protojson.MarshalOptions{UseProtoNames: protoNameTypes[policyType]}.Marshal(msg)
	if err != nil {
		return "", err
	}
	// protojson output spacing is deliberately unstable; re-indent for
	// deterministic bytes.
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		return "", err
	}
	return buf.String(), nil
}

var slugStrip = regexp.MustCompile(`[^a-z0-9]+`)

// Slugify derives the metadata.name slug from a policy display name.
func Slugify(name string) string {
	s := slugStrip.ReplaceAllString(strings.ToLower(name), "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return "policy"
	}
	if len(s) > 63 {
		s = strings.Trim(s[:63], "-")
	}
	return s
}

// BuildPolicyDoc renders one Policy resource as canonical JSON. The envelope
// keys are emitted in fixed order by construction (protojson field order
// inside each part), so output is byte-stable.
func BuildPolicyDoc(p *models.Policy, slug string) ([]byte, error) {
	canon, err := CanonicalizeContent(p.Type, p.Content)
	if err != nil {
		return nil, fmt.Errorf("policy %q: %w", p.Name, err)
	}
	var st structpb.Struct
	if uerr := protojson.Unmarshal([]byte(canon), &st); uerr != nil {
		return nil, uerr
	}
	meta, err := protojson.Marshal(&exportpb.ResourceMetadata{
		Name:        slug,
		DisplayName: p.Name,
		Description: p.Description,
	})
	if err != nil {
		return nil, err
	}
	spec, err := protojson.Marshal(&exportpb.PolicySpec{Type: p.Type, Content: &st})
	if err != nil {
		return nil, err
	}
	return assembleDoc("Policy", meta, spec), nil
}

// BuildBindingDoc renders one PolicyBinding resource as canonical JSON.
func BuildBindingDoc(policySlug, policyName, groupName string, priority int) ([]byte, error) {
	if priority < 0 || priority > 1<<30 {
		return nil, fmt.Errorf("priority %d out of range", priority)
	}
	meta, err := protojson.Marshal(&exportpb.ResourceMetadata{
		Name: Slugify(policyName + "-" + groupName),
	})
	if err != nil {
		return nil, err
	}
	spec, err := protojson.Marshal(&exportpb.BindingSpec{
		Policy:   policySlug,
		Group:    groupName,
		Priority: int32(priority),
	})
	if err != nil {
		return nil, err
	}
	return assembleDoc("PolicyBinding", meta, spec), nil
}

func assembleDoc(kind string, meta, spec []byte) []byte {
	var b bytes.Buffer
	b.WriteString(`{"apiVersion":"` + APIVersion + `","kind":"` + kind + `","metadata":`)
	b.Write(meta)
	b.WriteString(`,"spec":`)
	b.Write(spec)
	b.WriteString("}")
	return b.Bytes()
}

// MarshalBundle converts canonical JSON documents into one multi-document
// YAML stream.
func MarshalBundle(docs [][]byte) ([]byte, error) {
	var out bytes.Buffer
	for i, d := range docs {
		if i > 0 {
			out.WriteString("---\n")
		}
		y, err := JSONToYAML(d)
		if err != nil {
			return nil, err
		}
		out.Write(y)
	}
	return out.Bytes(), nil
}

// MarshalBundleJSON renders the documents as a JSON array (the `?format=json`
// variant).
func MarshalBundleJSON(docs [][]byte) ([]byte, error) {
	raws := make([]json.RawMessage, len(docs))
	for i, d := range docs {
		raws[i] = d
	}
	return json.MarshalIndent(raws, "", "  ")
}

// ParseResource decodes one JSON document into the envelope, mapping the wire
// key "spec" onto the kind-specific submessage. Unknown envelope fields are
// rejected (strict import boundary).
func ParseResource(doc []byte) (*exportpb.Resource, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(doc, &m); err != nil {
		return nil, err
	}
	var kind string
	if raw, ok := m["kind"]; ok {
		if err := json.Unmarshal(raw, &kind); err != nil {
			return nil, fmt.Errorf("kind: %w", err)
		}
	}
	spec, hasSpec := m["spec"]
	delete(m, "spec")
	switch kind {
	case "Policy":
		if hasSpec {
			m["policy"] = spec
		}
	case "PolicyBinding":
		if hasSpec {
			m["binding"] = spec
		}
	default:
		return nil, fmt.Errorf("unsupported kind %q (expected Policy or PolicyBinding)", kind)
	}
	flat, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	var res exportpb.Resource
	// DiscardUnknown deliberately NOT set: unknown envelope fields are errors.
	if err := protojson.Unmarshal(flat, &res); err != nil {
		return nil, err
	}
	if res.ApiVersion != APIVersion {
		return nil, fmt.Errorf("unsupported apiVersion %q (expected %s)", res.ApiVersion, APIVersion)
	}
	return &res, nil
}

// ContentJSON extracts the policy content document from a parsed resource.
func ContentJSON(spec *exportpb.PolicySpec) (string, error) {
	if spec.Content == nil {
		return "{}", nil
	}
	b, err := protojson.Marshal(spec.Content)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
