// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

// Accessibility lint gate for the web UI.
//
// The UI targets WCAG 2.2 Level AA (see docs/ACCESSIBILITY.md and CLAUDE.md).
// This config runs eslint-plugin-jsx-a11y's recommended rules over the
// hand-written source so accessibility regressions fail lint/CI rather than
// slipping through review. It is intentionally scoped to accessibility only —
// it is not a general TypeScript/React style linter.

import jsxA11y from "eslint-plugin-jsx-a11y";
import tsParser from "@typescript-eslint/parser";
import globals from "globals";

// Inert rule: defines a rule name without reporting anything. Used to keep
// pre-existing inline `eslint-disable` directives that reference linters outside
// this a11y gate's scope (react-hooks, typescript-eslint) from erroring as
// "definition for rule not found". Wiring up those full linters is a separate task.
const inertRule = { meta: {}, create: () => ({}) };

export default [
  {
    // Generated protobuf clients and build output are not hand-written UI.
    ignores: ["src/generated/**", "dist/**", "node_modules/**"],
  },
  {
    files: ["src/**/*.{ts,tsx}"],
    plugins: {
      "jsx-a11y": jsxA11y,
      "react-hooks": { rules: { "exhaustive-deps": inertRule } },
      "@typescript-eslint": { rules: { "no-explicit-any": inertRule } },
    },
    linterOptions: {
      // The inert shim rules above never report, so their disable directives
      // read as "unused"; don't flag them — they belong to future full linters.
      reportUnusedDisableDirectives: "off",
    },
    languageOptions: {
      parser: tsParser,
      parserOptions: {
        ecmaFeatures: { jsx: true },
        sourceType: "module",
      },
      globals: { ...globals.browser },
    },
    rules: {
      ...jsxA11y.flatConfigs.recommended.rules,
      // autoFocus is used deliberately to place focus into freshly-opened
      // modals/dialogs and the dedicated login screen — legitimate focus
      // management, not the page-load focus-stealing this rule guards against.
      // Keep it visible as a warning so new uses get reviewed, without failing
      // the build on the intended ones or forcing behavior-changing edits.
      "jsx-a11y/no-autofocus": "warn",
    },
  },
];
