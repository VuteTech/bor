// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import (
	"fmt"
	"strings"
)

// richTok is one token of a firewalld rich-language rule: either a bare keyword
// (hasVal=false) or a key="value" pair (hasVal=true).
type richTok struct {
	key    string
	val    string
	hasVal bool
}

// tokenizeRich splits a rich-language rule into tokens, honouring double quotes.
func tokenizeRich(s string) ([]richTok, error) {
	var toks []richTok
	i, n := 0, len(s)
	for i < n {
		for i < n && (s[i] == ' ' || s[i] == '\t') {
			i++
		}
		if i >= n {
			break
		}
		start := i
		for i < n && s[i] != ' ' && s[i] != '\t' && s[i] != '=' {
			i++
		}
		key := s[start:i]
		if i < n && s[i] == '=' {
			i++ // consume '='
			if i < n && s[i] == '"' {
				i++ // opening quote
				vstart := i
				for i < n && s[i] != '"' {
					i++
				}
				if i >= n {
					return nil, fmt.Errorf("unterminated quote")
				}
				toks = append(toks, richTok{key, s[vstart:i], true})
				i++ // closing quote
			} else {
				vstart := i
				for i < n && s[i] != ' ' && s[i] != '\t' {
					i++
				}
				toks = append(toks, richTok{key, s[vstart:i], true})
			}
		} else {
			toks = append(toks, richTok{key, "", false})
		}
	}
	return toks, nil
}

// richRuleToXML converts a firewalld rich-language rule string into its zone
// XML <rule>…</rule> form. It covers the common grammar (family/priority,
// source/destination with optional "not", the standard elements, log/audit with
// limits, and the terminal action). Unrecognised tokens produce an error;
// callers rely on firewall-cmd --check-config as the final validator, so an
// imperfect conversion is reported as an error rather than silently applied.
func richRuleToXML(rule string) (string, error) {
	toks, err := tokenizeRich(strings.TrimSpace(rule))
	if err != nil {
		return "", err
	}
	if len(toks) == 0 || toks[0].key != "rule" || toks[0].hasVal {
		return "", fmt.Errorf("rule must begin with the keyword 'rule'")
	}

	attr := func(name, val string) string { return fmt.Sprintf("%s=%q", name, xmlEscape(val)) }

	var ruleAttrs []string
	var children []string
	i := 1

	// consumeAttrs collects following key="value" tokens whose key is allowed.
	consumeAttrs := func(allowed map[string]bool) []string {
		var out []string
		for i < len(toks) && toks[i].hasVal && allowed[toks[i].key] {
			out = append(out, attr(toks[i].key, toks[i].val))
			i++
		}
		return out
	}
	// consumeLimit emits a nested <limit .../> if the next tokens are a limit.
	consumeLimit := func() string {
		if i < len(toks) && !toks[i].hasVal && toks[i].key == "limit" {
			i++
			la := consumeAttrs(map[string]bool{"value": true})
			return "<limit " + strings.Join(la, " ") + "/>"
		}
		return ""
	}
	wrap := func(tag, inner string) string {
		if inner == "" {
			return "<" + tag + "/>"
		}
		return "<" + tag + ">" + inner + "</" + tag + ">"
	}

	for i < len(toks) {
		t := toks[i]
		switch {
		case t.key == "family" && t.hasVal:
			ruleAttrs = append(ruleAttrs, attr("family", t.val))
			i++
		case t.key == "priority" && t.hasVal:
			ruleAttrs = append(ruleAttrs, attr("priority", t.val))
			i++
		case (t.key == "source" || t.key == "destination") && !t.hasVal:
			block := t.key
			i++
			invert := ""
			if i < len(toks) && !toks[i].hasVal && toks[i].key == "not" {
				invert = ` invert="True"`
				i++
			}
			sattrs := consumeAttrs(map[string]bool{"address": true, "mac": true, "ipset": true})
			if len(sattrs) == 0 {
				return "", fmt.Errorf("%s requires address, mac or ipset", block)
			}
			children = append(children, fmt.Sprintf("<%s%s %s/>", block, invert, strings.Join(sattrs, " ")))
		case t.key == "service" && !t.hasVal:
			i++
			a := consumeAttrs(map[string]bool{"name": true})
			children = append(children, "<service "+strings.Join(a, " ")+"/>")
		case t.key == "port" && !t.hasVal:
			i++
			a := consumeAttrs(map[string]bool{"port": true, "protocol": true})
			children = append(children, "<port "+strings.Join(a, " ")+"/>")
		case t.key == "source-port" && !t.hasVal:
			i++
			a := consumeAttrs(map[string]bool{"port": true, "protocol": true})
			children = append(children, "<source-port "+strings.Join(a, " ")+"/>")
		case t.key == "protocol" && !t.hasVal:
			i++
			a := consumeAttrs(map[string]bool{"value": true})
			children = append(children, "<protocol "+strings.Join(a, " ")+"/>")
		case t.key == "icmp-block" && !t.hasVal:
			i++
			a := consumeAttrs(map[string]bool{"name": true})
			children = append(children, "<icmp-block "+strings.Join(a, " ")+"/>")
		case t.key == "icmp-type" && !t.hasVal:
			i++
			a := consumeAttrs(map[string]bool{"name": true})
			children = append(children, "<icmp-type "+strings.Join(a, " ")+"/>")
		case t.key == "masquerade" && !t.hasVal:
			i++
			children = append(children, "<masquerade/>")
		case t.key == "forward-port" && !t.hasVal:
			i++
			a := consumeAttrs(map[string]bool{"port": true, "protocol": true, "to-port": true, "to-addr": true})
			children = append(children, "<forward-port "+strings.Join(a, " ")+"/>")
		case t.key == "log" && !t.hasVal:
			i++
			a := consumeAttrs(map[string]bool{"prefix": true, "level": true})
			lim := consumeLimit()
			open := "<log"
			if len(a) > 0 {
				open += " " + strings.Join(a, " ")
			}
			if lim == "" {
				children = append(children, open+"/>")
			} else {
				children = append(children, open+">"+lim+"</log>")
			}
		case t.key == "audit" && !t.hasVal:
			i++
			children = append(children, wrap("audit", consumeLimit()))
		case t.key == "accept" && !t.hasVal:
			i++
			children = append(children, wrap("accept", consumeLimit()))
		case t.key == "drop" && !t.hasVal:
			i++
			children = append(children, wrap("drop", consumeLimit()))
		case t.key == "reject" && !t.hasVal:
			i++
			ra := consumeAttrs(map[string]bool{"type": true})
			lim := consumeLimit()
			open := "<reject"
			if len(ra) > 0 {
				open += " " + strings.Join(ra, " ")
			}
			if lim == "" {
				children = append(children, open+"/>")
			} else {
				children = append(children, open+">"+lim+"</reject>")
			}
		case t.key == "mark" && !t.hasVal:
			i++
			a := consumeAttrs(map[string]bool{"set": true})
			lim := consumeLimit()
			open := "<mark " + strings.Join(a, " ")
			if lim == "" {
				children = append(children, open+"/>")
			} else {
				children = append(children, open+">"+lim+"</mark>")
			}
		default:
			return "", fmt.Errorf("unrecognised token %q in rich rule", t.key)
		}
	}

	open := "<rule"
	if len(ruleAttrs) > 0 {
		open += " " + strings.Join(ruleAttrs, " ")
	}
	if len(children) == 0 {
		return "", fmt.Errorf("rich rule has no elements")
	}
	return open + ">" + strings.Join(children, "") + "</rule>", nil
}
