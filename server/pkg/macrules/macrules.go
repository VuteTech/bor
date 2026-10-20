// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

// Package macrules holds the rules the SELinux and AppArmor policy types
// share between the server (policy validation) and the agent
// (re-validation of server data before a value becomes a path component,
// a command argument or policy text): name syntax, the protected and risky
// lists, the structural checks of CIL modules and AppArmor profiles, and
// the classification of profiles other software owns.
package macrules

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

// BorPrefix is the prefix of every module, profile file and profile name
// Bor deploys, so Bor can never replace a vendor object.
const BorPrefix = "bor-"

// Limits shared by both types.
const (
	// MaxTextBytes bounds a CIL module, a profile or a local override.
	MaxTextBytes = 64 * 1024
	// MaxCustomItems bounds the custom modules or profiles of one policy.
	MaxCustomItems = 32
	// MaxDescriptionLength bounds free-text descriptions.
	MaxDescriptionLength = 512
	// maxNesting bounds the depth of parentheses or braces in policy text.
	maxNesting = 32
)

var (
	customNameRE  = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,63}$`)
	booleanRE     = regexp.MustCompile(`^[a-z][a-z0-9_]{0,127}$`)
	typeRE        = regexp.MustCompile(`^[a-z][a-z0-9_]{0,126}_t$`)
	moduleRE      = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,63}$`)
	seuserRE      = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	loginRE       = regexp.MustCompile(`^%?[a-z_][a-z0-9_.@-]{0,31}\$?$`)
	rangeRE       = regexp.MustCompile(`^s[0-9]{1,4}(-s[0-9]{1,4})?(:c[0-9]{1,4}(\.c[0-9]{1,4})?(,c[0-9]{1,4}(\.c[0-9]{1,4})?)*)?$`)
	profileFileRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.@+-]{0,127}$`)
	globRE        = regexp.MustCompile(`^[A-Za-z0-9_.@+*?-]{1,128}$`)
	homeDirRE     = regexp.MustCompile(`^/[A-Za-z0-9_./-]*/$`)
)

// ValidateCustomName checks the bare name of a Bor-deployed module or
// profile; the agent adds the "bor-" prefix.
func ValidateCustomName(name string) error {
	if name == "" {
		return fmt.Errorf("name must not be empty")
	}
	if !customNameRE.MatchString(name) {
		return fmt.Errorf("name %q may contain lowercase letters, digits, '_', '.' and '-' only (at most 64 characters)", name)
	}
	if strings.HasPrefix(name, BorPrefix) {
		return fmt.Errorf("name %q must not start with %q; the prefix is added automatically", name, BorPrefix)
	}
	if name == "." || name == ".." || strings.Contains(name, "..") {
		return fmt.Errorf("name %q is not permitted", name)
	}
	return nil
}

// ─── SELinux ─────────────────────────────────────────────────────────────────

// ValidateBooleanName checks an SELinux boolean name.
func ValidateBooleanName(name string) error {
	if name == "" {
		return fmt.Errorf("boolean name must not be empty")
	}
	if !booleanRE.MatchString(name) {
		return fmt.Errorf("boolean name %q is malformed", name)
	}
	return nil
}

// ValidateDomain checks an SELinux type name ("httpd_t").
func ValidateDomain(name string) error {
	if name == "" {
		return fmt.Errorf("type name must not be empty")
	}
	if !typeRE.MatchString(name) {
		return fmt.Errorf("type name %q must be lowercase and end in _t", name)
	}
	return nil
}

// ValidateModuleName checks the name of an existing policy module.
func ValidateModuleName(name string) error {
	if name == "" {
		return fmt.Errorf("module name must not be empty")
	}
	if !moduleRE.MatchString(name) || strings.Contains(name, "..") {
		return fmt.Errorf("module name %q is malformed", name)
	}
	return nil
}

// ValidateSeuser checks an SELinux user name ("user_u", "staff_u").
func ValidateSeuser(name string) error {
	if name == "" {
		return fmt.Errorf("SELinux user must not be empty")
	}
	if !seuserRE.MatchString(name) {
		return fmt.Errorf("SELinux user %q is malformed", name)
	}
	if name == "system_u" {
		return fmt.Errorf("logins cannot be mapped to system_u")
	}
	return nil
}

// ValidateLogin checks a login name or "%group" of a login mapping.
func ValidateLogin(name string) error {
	if name == "" {
		return fmt.Errorf("login must not be empty")
	}
	if name == "__default__" || name == "root" {
		return fmt.Errorf("the %s mapping cannot be changed by a policy", name)
	}
	if !loginRE.MatchString(name) {
		return fmt.Errorf("login %q is malformed", name)
	}
	return nil
}

// ValidateRange checks an MLS/MCS range ("s0", "s0-s0:c0.c1023"); empty
// is allowed and means the default.
func ValidateRange(r string) error {
	if r == "" {
		return nil
	}
	if !rangeRE.MatchString(r) {
		return fmt.Errorf("range %q is malformed", r)
	}
	return nil
}

// fileContextRoots are prefixes a file context rule may not label as a
// whole; relabelling them breaks the system.
var fileContextRoots = map[string]struct{}{
	"/": {}, "/usr": {}, "/etc": {}, "/var": {}, "/boot": {}, "/dev": {}, "/proc": {},
	"/sys": {}, "/run": {}, "/lib": {}, "/lib64": {}, "/bin": {}, "/sbin": {}, "/root": {},
	"/home": {}, "/tmp": {}, "/var/lib": {}, "/var/log": {}, "/usr/bin": {}, "/usr/lib": {},
	"/usr/lib64": {}, "/usr/sbin": {}, "/usr/share": {}, "/etc/selinux": {},
}

// ValidateFileContextRegex checks the path regular expression of a file
// context rule and returns its literal prefix, the path restorecon walks.
func ValidateFileContextRegex(expr string) (prefix string, err error) {
	if expr == "" {
		return "", fmt.Errorf("path must not be empty")
	}
	if len(expr) > 1024 || strings.ContainsAny(expr, "\n\r\x00") {
		return "", fmt.Errorf("path expression is too long or contains control characters")
	}
	if !strings.HasPrefix(expr, "/") {
		return "", fmt.Errorf("path expression %q must be absolute", expr)
	}
	if _, err := regexp.Compile("^" + expr + "$"); err != nil {
		return "", fmt.Errorf("path expression %q is not a valid regular expression: %v", expr, err)
	}
	prefix = LiteralPrefix(expr)
	clean := strings.TrimRight(prefix, "/")
	if clean == "" {
		clean = "/"
	}
	if _, bad := fileContextRoots[clean]; bad {
		return "", fmt.Errorf("path expression %q would relabel %s as a whole", expr, clean)
	}
	if strings.Contains(prefix, "/../") || strings.HasSuffix(prefix, "/..") {
		return "", fmt.Errorf("path expression %q is not permitted", expr)
	}
	return prefix, nil
}

// LiteralPrefix returns the part of a path regular expression before its
// first metacharacter, cut back to a directory boundary.
func LiteralPrefix(expr string) string {
	end := strings.IndexAny(expr, `.^$*+?()[]{}|\`)
	if end < 0 {
		return expr
	}
	prefix := expr[:end]
	// "/opt/corpapp(/.*)?" keeps the whole component; "/opt/corp.*" and
	// "/srv/site[0-9]+/x" end inside a component and are cut back to the
	// directory.
	if !strings.HasPrefix(expr[end:], "(/") {
		if i := strings.LastIndex(prefix, "/"); i >= 0 {
			prefix = prefix[:i+1]
		}
	}
	if prefix == "" {
		prefix = "/"
	}
	return prefix
}

// ProtectedBooleans are booleans no policy may set: secure_mode_policyload
// locks the policy until reboot, including against Bor itself.
var ProtectedBooleans = map[string]struct{}{
	"secure_mode_policyload": {},
}

// IsProtectedBoolean reports whether no policy may set the boolean.
func IsProtectedBoolean(name string) bool {
	_, ok := ProtectedBooleans[name]
	return ok
}

// riskyBooleanValues are boolean values that can lock users out or break
// desktop applications fleet-wide; the editor asks for a confirmation.
var riskyBooleanValues = map[string]bool{
	"unconfined_login":           false,
	"deny_execmem":               true,
	"deny_ptrace":                true,
	"selinuxuser_execstack":      false,
	"selinuxuser_execmod":        false,
	"secure_mode_insmod":         true,
	"domain_kernel_load_modules": false,
	"ssh_sysadm_login":           true,
}

// IsRiskyBoolean reports whether setting the boolean to value deserves a
// confirmation.
func IsRiskyBoolean(name string, value bool) bool {
	if v, ok := riskyBooleanValues[name]; ok && v == value {
		return true
	}
	if strings.HasPrefix(name, "xguest_") {
		return true
	}
	if strings.HasPrefix(name, "use_") && strings.HasSuffix(name, "_home_dirs") && !value {
		return true
	}
	return false
}

// riskyModules are modules whose disabling can lock users or the agent out.
var riskyModules = map[string]struct{}{
	"unconfined": {}, "xserver": {}, "systemd": {}, "init": {}, "dbus": {}, "polkit": {},
	"sudo": {}, "su": {}, "login": {}, "ssh": {}, "sshd": {}, "networkmanager": {},
	"selinuxutil": {}, "userdomain": {}, "authlogin": {}, "kernel": {}, "logging": {},
	"staff": {}, "sysadm": {}, "unprivuser": {}, "xdm": {}, "gdm": {}, "sddm": {},
}

// IsRiskyModule reports whether disabling the module deserves a
// confirmation.
func IsRiskyModule(name string) bool {
	_, ok := riskyModules[name]
	return ok
}

// riskyPermissiveDomains are domains whose permissive mode removes the
// protection of the whole login path.
var riskyPermissiveDomains = map[string]struct{}{
	"init_t": {}, "kernel_t": {}, "unconfined_t": {}, "sshd_t": {}, "xdm_t": {}, "sudo_t": {},
	"login_t": {}, "systemd_logind_t": {}, "polkit_t": {},
}

// IsRiskyPermissiveDomain reports whether making the domain permissive
// deserves a confirmation.
func IsRiskyPermissiveDomain(name string) bool {
	_, ok := riskyPermissiveDomains[name]
	return ok
}

// cilAllowedStatements are the top-level CIL statements a Bor module may
// use: rules, type and attribute declarations, booleans, file and port
// contexts, login mappings, and the containers CIL sources are organised
// in. Statements that restructure the base policy (classes, sids, users
// and roles, MLS, policy capabilities, kernel object contexts) are not
// accepted.
var cilAllowedStatements = map[string]struct{}{
	"allow": {}, "auditallow": {}, "dontaudit": {}, "neverallow": {},
	"allowx": {}, "auditallowx": {}, "dontauditx": {}, "neverallowx": {}, "permissionx": {},
	"type": {}, "typealias": {}, "typealiasactual": {}, "typeattribute": {}, "typeattributeset": {},
	"typetransition": {}, "typechange": {}, "typemember": {}, "typebounds": {}, "typepermissive": {},
	"expandtypeattribute": {},
	"roletype":            {}, "roleattribute": {}, "roleattributeset": {},
	"boolean": {}, "booleanif": {}, "tunable": {}, "tunableif": {},
	"classpermission": {}, "classpermissionset": {}, "classmap": {}, "classmapping": {},
	"filecon": {}, "portcon": {}, "rangetransition": {},
	"selinuxuser": {}, "selinuxuserdefault": {},
	"optional": {}, "block": {}, "blockinherit": {}, "blockabstract": {}, "macro": {}, "call": {}, "in": {},
}

// CheckCIL checks a Bor CIL module: size, balanced parentheses (ignoring
// comments and quoted strings), nesting depth and the top-level statement
// allowlist. Semantic errors are left to semodule on the node.
func CheckCIL(text string) error {
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("the module is empty")
	}
	if len(text) > MaxTextBytes {
		return fmt.Errorf("the module is larger than %d KiB", MaxTextBytes/1024)
	}
	if strings.ContainsRune(text, 0) {
		return fmt.Errorf("the module contains a NUL byte")
	}
	depth := 0
	inString := false
	inComment := false
	statements := 0
	var tok strings.Builder
	collecting := false
	for i := 0; i < len(text); i++ {
		c := text[i]
		switch {
		case inComment:
			if c == '\n' {
				inComment = false
			}
			continue
		case inString:
			if c == '"' {
				inString = false
			}
			continue
		case c == ';':
			inComment = true
			continue
		case c == '"':
			inString = true
			continue
		}
		if collecting {
			if c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '(' || c == ')' {
				collecting = false
				stmt := tok.String()
				if _, ok := cilAllowedStatements[stmt]; !ok {
					return fmt.Errorf("statement %q is not permitted in a Bor module", stmt)
				}
			} else {
				tok.WriteByte(c)
			}
		}
		switch c {
		case '(':
			depth++
			if depth > maxNesting {
				return fmt.Errorf("the module nests deeper than %d levels", maxNesting)
			}
			if depth == 1 {
				statements++
				tok.Reset()
				collecting = true
			}
		case ')':
			depth--
			if depth < 0 {
				return fmt.Errorf("unbalanced parentheses: ')' without '('")
			}
		default:
			if depth == 0 && c != ' ' && c != '\t' && c != '\n' && c != '\r' {
				return fmt.Errorf("text outside of a statement near %q", snippet(text, i))
			}
		}
	}
	if inString {
		return fmt.Errorf("unterminated string")
	}
	if depth != 0 {
		return fmt.Errorf("unbalanced parentheses: %d '(' without ')'", depth)
	}
	if statements == 0 {
		return fmt.Errorf("the module has no statement")
	}
	return nil
}

func snippet(text string, at int) string {
	end := min(at+16, len(text))
	return strings.TrimSpace(text[at:end])
}

// ─── AppArmor ────────────────────────────────────────────────────────────────

// reservedProfileFiles are entries of /etc/apparmor.d that are not profile
// files.
var reservedProfileFiles = map[string]struct{}{
	"abi": {}, "abstractions": {}, "tunables": {}, "local": {}, "disable": {},
	"force-complain": {}, "cache": {}, "cache.d": {}, "README": {},
}

// IsReservedProfileFile reports whether the entry is a directory or a
// file of /etc/apparmor.d that holds no profile.
func IsReservedProfileFile(name string) bool {
	_, ok := reservedProfileFiles[name]
	return ok
}

// ValidateProfileFile checks the name of a profile file under
// /etc/apparmor.d. It is a single path component.
func ValidateProfileFile(name string) error {
	if name == "" {
		return fmt.Errorf("profile file name must not be empty")
	}
	if !profileFileRE.MatchString(name) || strings.Contains(name, "..") {
		return fmt.Errorf("profile file name %q is malformed", name)
	}
	if IsReservedProfileFile(name) {
		return fmt.Errorf("%q is not a profile file", name)
	}
	if strings.HasSuffix(name, ".dpkg-new") || strings.HasSuffix(name, ".dpkg-old") ||
		strings.HasSuffix(name, ".dpkg-dist") || strings.HasSuffix(name, ".rpmnew") ||
		strings.HasSuffix(name, ".rpmsave") || strings.HasSuffix(name, "~") {
		return fmt.Errorf("%q is a package manager leftover, not a profile", name)
	}
	return nil
}

// IsGlob reports whether a profile match pattern contains wildcards.
func IsGlob(match string) bool {
	return strings.ContainsAny(match, "*?")
}

// ValidateProfileMatch checks a profile file name or a glob with * and ?.
func ValidateProfileMatch(match string) error {
	if match == "" {
		return fmt.Errorf("profile match must not be empty")
	}
	if !IsGlob(match) {
		return ValidateProfileFile(match)
	}
	if !globRE.MatchString(match) || strings.Contains(match, "/") {
		return fmt.Errorf("profile pattern %q may contain letters, digits, '_', '.', '@', '+', '-', '*' and '?' only", match)
	}
	if _, err := path.Match(match, "x"); err != nil {
		return fmt.Errorf("profile pattern %q is malformed", match)
	}
	return nil
}

// MatchProfileFile reports whether a profile file name matches an exact
// name or a glob.
func MatchProfileFile(match, file string) bool {
	if !IsGlob(match) {
		return match == file
	}
	ok, err := path.Match(match, file)
	return err == nil && ok
}

// GlobSpecificity orders globs: more literal characters win when two
// globs match the same file.
func GlobSpecificity(match string) int {
	n := 0
	for _, r := range match {
		if r != '*' && r != '?' {
			n++
		}
	}
	return n
}

// protectedProfiles are profiles no policy may disable: the names the
// user namespace restriction depends on.
var protectedProfiles = map[string]struct{}{
	"unconfined": {}, "unprivileged_userns": {}, "bwrap-userns-restrict": {},
}

// IsProtectedProfile reports whether no policy may disable the profile.
func IsProtectedProfile(file string) bool {
	_, ok := protectedProfiles[file]
	return ok
}

// ManagedBy classifies a loaded profile or profile file by who owns it:
// "bor" for Bor-deployed files, "snapd", "docker", "lxd" and "libvirt" for
// profiles their owners generate (never managed), "vendor" otherwise.
func ManagedBy(profileName, file string) string {
	switch {
	case strings.HasPrefix(file, BorPrefix) || strings.HasPrefix(profileName, BorPrefix):
		return "bor"
	case strings.HasPrefix(profileName, "snap.") || strings.HasPrefix(profileName, "snap-update-ns.") || strings.HasPrefix(profileName, "/snap/"):
		return "snapd"
	case strings.HasPrefix(profileName, "docker-") || strings.HasPrefix(file, "docker-"):
		return "docker"
	case strings.HasPrefix(profileName, "lxd-") || strings.HasPrefix(profileName, "lxc-") || strings.HasPrefix(file, "lxd-"):
		return "lxd"
	case strings.HasPrefix(profileName, "libvirt-") || strings.HasPrefix(file, "libvirt-"):
		return "libvirt"
	}
	return "vendor"
}

// IsDynamicManaged reports whether the owner regenerates the profile, so
// Bor must not manage it.
func IsDynamicManaged(managedBy string) bool {
	switch managedBy {
	case "snapd", "docker", "lxd", "libvirt":
		return true
	}
	return false
}

// CheckProfileText checks a Bor profile: size, balanced braces (ignoring
// comments and quoted strings), nesting depth, and that every top-level
// profile declaration names bor-<name> (so the file can never replace a
// vendor profile). Syntax is left to apparmor_parser on the node.
func CheckProfileText(name, text string) error {
	if err := checkBraces(text, "profile"); err != nil {
		return err
	}
	want := BorPrefix + name
	found := 0
	for _, line := range topLevelLines(text) {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		switch {
		case fields[0] == "profile":
			if len(fields) < 2 || fields[1] != want {
				return fmt.Errorf("a top-level profile must be declared as %q, found %q", "profile "+want, strings.TrimSpace(line))
			}
			found++
		case strings.HasPrefix(fields[0], "/") || strings.HasPrefix(fields[0], "@{"):
			return fmt.Errorf("a bare attachment profile (%q) is not permitted; declare it as %q", fields[0], "profile "+want+" "+fields[0])
		case fields[0] == "abi", fields[0] == "include", fields[0] == "#include", strings.HasPrefix(fields[0], "@"), strings.HasPrefix(fields[0], "$"), fields[0] == "alias":
			// preamble
		default:
			return fmt.Errorf("unexpected text outside of the profile: %q", strings.TrimSpace(line))
		}
	}
	if found == 0 {
		return fmt.Errorf("the profile text must declare %q", "profile "+want)
	}
	if found > 1 {
		return fmt.Errorf("the profile text declares %q more than once", "profile "+want)
	}
	return nil
}

// CheckLocalOverrideText checks rules destined for a local/ include:
// size and balanced braces. The vendor profile is re-checked by the parser
// on the node after the block is written.
func CheckLocalOverrideText(text string) error {
	return checkBraces(text, "override")
}

// checkBraces checks size, NUL bytes, nesting and brace balance of
// AppArmor policy text.
func checkBraces(text, what string) error {
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("the %s is empty", what)
	}
	if len(text) > MaxTextBytes {
		return fmt.Errorf("the %s is larger than %d KiB", what, MaxTextBytes/1024)
	}
	if strings.ContainsRune(text, 0) {
		return fmt.Errorf("the %s contains a NUL byte", what)
	}
	depth := 0
	inString := false
	inComment := false
	for i := 0; i < len(text); i++ {
		c := text[i]
		switch {
		case inComment:
			if c == '\n' {
				inComment = false
			}
			continue
		case inString:
			switch c {
			case '"':
				inString = false
			case '\\':
				i++
			}
			continue
		case c == '#' && !strings.HasPrefix(text[i:], "#include"):
			inComment = true
			continue
		case c == '"':
			inString = true
			continue
		}
		switch c {
		case '{':
			depth++
			if depth > maxNesting {
				return fmt.Errorf("the %s nests deeper than %d levels", what, maxNesting)
			}
		case '}':
			depth--
			if depth < 0 {
				return fmt.Errorf("unbalanced braces in the %s: '}' without '{'", what)
			}
		}
	}
	if inString {
		return fmt.Errorf("unterminated string in the %s", what)
	}
	if depth != 0 {
		return fmt.Errorf("unbalanced braces in the %s: %d '{' without '}'", what, depth)
	}
	return nil
}

// topLevelLines returns the lines of profile text that sit outside every
// brace block, without comments.
func topLevelLines(text string) []string {
	var out []string
	depth := 0
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if i := strings.Index(trimmed, "#"); i >= 0 && !strings.HasPrefix(trimmed[i:], "#include") {
			trimmed = strings.TrimSpace(trimmed[:i])
		}
		if trimmed == "" {
			continue
		}
		if depth == 0 {
			out = append(out, trimmed)
		}
		for _, c := range trimmed {
			switch c {
			case '{':
				depth++
			case '}':
				depth--
			}
		}
	}
	return out
}

// ValidateHomeDir checks an extra home directory root for @{HOMEDIRS}.
func ValidateHomeDir(dir string) error {
	if dir == "" {
		return fmt.Errorf("home directory must not be empty")
	}
	if !homeDirRE.MatchString(dir) || strings.Contains(dir, "..") || strings.Contains(dir, "//") {
		return fmt.Errorf("home directory %q must be an absolute path ending in '/' without special characters", dir)
	}
	if dir == "/" {
		return fmt.Errorf("the root directory cannot be a home directory root")
	}
	return nil
}

// ProfileFileName returns the file under /etc/apparmor.d of a Bor custom
// profile.
func ProfileFileName(name string) string {
	return BorPrefix + name
}

// ModuleName returns the semodule name of a Bor custom module.
func ModuleName(name string) string {
	return BorPrefix + name
}
