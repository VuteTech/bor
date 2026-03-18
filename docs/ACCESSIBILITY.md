# Accessibility — Bor Web UI

## Compliance

The Bor web UI targets **WCAG 2.2 Level AA**, which supersedes WCAG 2.1 and is the version tracked by Section 508 and EN 301 549.

All WCAG 2.2 AA success criteria that apply to this UI are satisfied.

---

## What is implemented

| Area | WCAG |
|---|---|
| Language declaration (`lang="en"` on `<html>`) | 1.1.1 / 3.1.1 |
| All icon-only buttons carry `aria-label` | 1.1.1 / 4.1.2 |
| Color is never the sole status differentiator — text labels accompany all colored indicators | 1.4.1 |
| Light / Dark / System theme toggle; persisted across sessions | 1.4.3 |
| High contrast mode (manual toggle + `prefers-contrast: more` auto-apply) | 1.4.3 / 1.4.11 |
| OS Windows High Contrast (`forced-colors: active`) compatible | 1.4.11 |
| Semantic structure via PF6 Nav, Table, Form (built-in ARIA roles) | 1.3.1 |
| All tables have an accessible name via `aria-label` | 1.3.1 |
| `autocomplete` attributes on all login and password fields | 1.3.5 |
| Form errors linked to inputs via `aria-describedby` + `aria-invalid` | 1.3.1 |
| Abbreviations expanded with `<abbr title="...">` on first use (MFA, TOTP, FIDO2, NFC) | Best practice |
| Custom tree-view is fully keyboard-operable | 2.1.1 |
| Modal focus trap (PF6 Modal) | 2.1.2 |
| Focus returns to trigger element when a modal closes | 2.1.1 |
| Skip navigation link (visible on focus, first focusable element) | 2.4.1 |
| Document title updated on every screen navigation | 2.4.2 |
| Focus rings: ≥2 px with ≥3:1 contrast in all three themes | 2.4.11 |
| All icon-only buttons have a minimum 24×24 CSS px target area | 2.5.8 |
| No CAPTCHA or cognitive test on login | 3.3.8 |
| All status messages announced via ARIA live regions (`LiveAlert` component) | 4.1.3 |
| `aria-label="Loading"` on all spinners | 4.1.3 |
| CSS transitions and animations disabled when `prefers-reduced-motion: reduce` is set | 2.3.3 |

---

## Testing

- **Automated**: axe-core via `@axe-core/react` (development) or `@axe-core/playwright` (CI).
- **Keyboard**: every interactive flow operable without a mouse.
- **Screen reader**: NVDA + Firefox on Linux; VoiceOver + Safari on macOS.
- **Visual**: high contrast and reduced motion tested via OS settings.
