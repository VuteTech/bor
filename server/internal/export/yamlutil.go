// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

// Package export implements policy export/import in the bor.dev/v1 YAML
// envelope format (docs/policy-export-import-plan.md). YAML and JSON are two
// syntaxes for the same protobuf schema (proto/export/export.proto); this
// file is the YAML<->JSON bridge with the import-boundary hardening rules:
// no aliases/anchors, bounded depth, bounded document count and size.
package export

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"gopkg.in/yaml.v3"
)

const (
	// MaxBundleBytes is the maximum accepted import payload size.
	MaxBundleBytes = 1 << 20 // 1 MiB
	maxDocuments   = 200
	maxDepth       = 64
)

// SplitAndConvert parses a YAML (or JSON — a YAML subset) bundle and returns
// one JSON blob per non-empty document.
func SplitAndConvert(data []byte) ([][]byte, error) {
	if len(data) > MaxBundleBytes {
		return nil, fmt.Errorf("bundle exceeds %d bytes", MaxBundleBytes)
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var docs [][]byte
	for {
		var node yaml.Node
		err := dec.Decode(&node)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("document %d: %w", len(docs)+1, err)
		}
		if node.Kind == 0 {
			continue // empty document
		}
		if len(docs) >= maxDocuments {
			return nil, fmt.Errorf("bundle exceeds %d documents", maxDocuments)
		}
		if cerr := checkNode(&node, 0); cerr != nil {
			return nil, fmt.Errorf("document %d: %w", len(docs)+1, cerr)
		}
		j, jerr := nodeToJSON(&node)
		if jerr != nil {
			return nil, fmt.Errorf("document %d: %w", len(docs)+1, jerr)
		}
		docs = append(docs, j)
	}
	if len(docs) == 0 {
		return nil, errors.New("no documents found")
	}
	return docs, nil
}

// checkNode enforces the import-boundary rules on the raw YAML tree.
func checkNode(n *yaml.Node, depth int) error {
	if depth > maxDepth {
		return fmt.Errorf("nesting deeper than %d levels", maxDepth)
	}
	if n.Kind == yaml.AliasNode {
		return errors.New("YAML aliases/anchors are not allowed")
	}
	for _, c := range n.Content {
		if err := checkNode(c, depth+1); err != nil {
			return err
		}
	}
	return nil
}

func nodeToJSON(n *yaml.Node) ([]byte, error) {
	var v any
	if err := n.Decode(&v); err != nil {
		return nil, err
	}
	norm, err := normalizeValue(v)
	if err != nil {
		return nil, err
	}
	if _, ok := norm.(map[string]any); !ok {
		return nil, errors.New("top-level document must be a mapping")
	}
	return json.Marshal(norm)
}

// normalizeValue restricts documents to the JSON data model.
func normalizeValue(v any) (any, error) {
	switch t := v.(type) {
	case nil, bool, string, int, int64, uint64, float64:
		return t, nil
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			nv, err := normalizeValue(val)
			if err != nil {
				return nil, err
			}
			out[k] = nv
		}
		return out, nil
	case map[any]any:
		return nil, errors.New("mapping keys must be strings")
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			nv, err := normalizeValue(val)
			if err != nil {
				return nil, err
			}
			out[i] = nv
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unsupported YAML value type %T", v)
	}
}

// JSONToYAML converts one JSON document to block-style YAML, preserving key
// order (yaml.v3 parses JSON directly and keeps mapping order in the node
// tree; clearing the style flags switches the output to block form while the
// encoder re-quotes any ambiguous scalars).
func JSONToYAML(j []byte) ([]byte, error) {
	var n yaml.Node
	if err := yaml.Unmarshal(j, &n); err != nil {
		return nil, err
	}
	clearStyle(&n)
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&n); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func clearStyle(n *yaml.Node) {
	n.Style = 0
	for _, c := range n.Content {
		clearStyle(c)
	}
}
