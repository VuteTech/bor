# AI-assisted contributions

This page sets out how Bor handles contributions made with the help of AI
coding assistants and other content-generating tools. In short: tools are
welcome, a human is always accountable, and tool use is disclosed.

The rules apply to everyone, whether you send a pull request, mail a patch,
or open an issue.

## Why the rules exist

Tools can produce a large volume of plausible-looking code, documentation
and bug reports quickly. Review time, on the other hand, is the scarcest
resource in the project. Bor manages security settings on other people's
desktops: a subtle mistake in a policy merge, a TLS setting or a polkit rule
ends up on every enrolled machine. The rules below keep the cost of
reviewing a contribution proportional to its value, and keep the trust
between contributors and reviewers intact.

## Scope

**Out of scope** - no disclosure needed:

- Spelling and grammar fixes, rephrasing a sentence.
- Typing aids: identifier completion, common boilerplate, trivial pattern
  completion in your editor.
- Purely mechanical transformations, such as renaming a variable.
- Formatters and fixers: `gofmt`, `goimports`, `golangci-lint --fix`.

Even then, mention the tool if knowing about it would help the reviewer.

**In scope** - the rest of this page applies when a meaningful part of a
contribution was produced by a tool rather than written by you. For
example:

- An assistant generated a function, a test file or a migration, even if
  you edited it afterwards.
- An assistant suggested the fix for a bug, or found the bug.
- The commit message, pull request description or documentation page was
  generated or translated by a tool.
- Code produced by a code generator that is not part of the build (generated
  code from `make proto` is part of the build and is out of scope).

If in doubt, assume the rules apply and disclose.

## You are accountable for everything you submit

The person who submits a contribution is responsible for all of it, no
matter how it was produced. Before you submit, you must:

- **Read and understand every line.** You must be able to explain what the
  code does, why it is correct, how it handles edge cases and errors, and
  why this approach was chosen over the alternatives. Be ready to answer
  review comments yourself.
- **Run it.** Build it, run the tests (`make test`), the linters
  (`make lint`, with 0 issues) and, for frontend changes,
  `npm run typecheck`. Test the behavior you changed on a real server or
  agent where that applies. See [Testing and linting](testing.md).
- **Check that it follows the project rules.** The conventions in
  [Contributing](contributing.md#coding-conventions) and the cryptographic
  and compliance rules in
  [Security and compliance rules](contributing.md#security-and-compliance-rules)
  apply in full. Generated code often reaches for weak defaults (SHA-1,
  disabled certificate verification, string-built SQL, logging of secrets);
  none of these are accepted.
- **Check the license.** Everything in Bor is licensed under
  LGPL-3.0-or-later. You must have the right to contribute the content under
  that license. Do not submit output that reproduces code from projects with
  incompatible licenses, and never give a tool proprietary or confidential
  code from elsewhere to work from.

An AI tool cannot take responsibility for a contribution, so it is never
listed as an author or co-author. Do not add `Co-authored-by:` (or similar)
lines for a tool, and do not configure a tool to commit or open pull
requests under its own identity.

## Disclose tool use

In the pull request description (or the cover letter of a patch series),
say briefly:

- Which tools you used and which parts of the change they affected.
- How you verified the result: which tests you ran, what you tried on a
  real server or agent.
- When most of a change came from a short prompt or a few prompts, include
  them. For longer sessions, summarize what you asked for and what kind of
  help you got.
- Anything you could **not** verify, stated explicitly.

## Using a tool to find and fix bugs

Automated analysis finds real bugs, and it also produces many reports that
turn out to be wrong. Maintainers cannot afford to triage unverified
reports. When you use a tool to look for bugs, follow at least these steps:

1. Note the commit you analyzed (`git rev-parse HEAD`).
2. For anything that is not trivial, confirm the bug is real: write a
   failing test or a reproducer against a running server or agent. If you
   cannot make it fail, stop there.
3. Write the fix. A report that comes with a working, tested fix is far more
   useful than a report alone.
4. Prove the fix: the failing test now passes, `make test` and `make lint`
   are clean, and no new warnings appear.
5. Commit it with a message that describes the problem, the cause and the
   solution.
6. Say what you could not do: if you could not reproduce the bug, build the
   fix or run it on a real system, write that in the pull request.
7. Decide whether the bug is a security vulnerability (for example
   authentication or RBAC bypass, privilege escalation on the endpoint,
   certificate or key handling, injection, secrets in logs). Vulnerabilities
   are **never** reported in a public issue or pull request; follow
   [Reporting security vulnerabilities](contributing.md#reporting-security-vulnerabilities).

Never let a tool send reports, open issues or submit pull requests on its
own. A person reviews the result and submits it.

## Bug reports and issues

The same rules apply to issues: describe what you observed yourself. If a
tool helped you analyze logs or code, say so, and include the evidence
(log lines, versions, reproduction steps) rather than the tool's
conclusions alone. Issues that consist of unverified tool output may be
closed without investigation.

## How maintainers handle tool-assisted contributions

As with any contribution, maintainers decide case by case. They may:

- Treat it like any other contribution.
- Review it with extra scrutiny, ask for additional tests, or review it at a
  lower priority. Expect scrutiny in proportion to how much of a change was
  generated.
- Ask you to explain parts of the change in your own words, to make sure you
  understand how it works.
- Close it without a detailed review if you cannot explain or defend it, if
  the origin was not disclosed, or if it is a large volume of low-value
  changes.

## Summary

| Do | Do not |
|---|---|
| Use tools that make you more productive | Submit code you cannot explain |
| Review, build and test everything yourself | Rely on a tool for security-critical code without careful manual review and testing |
| Describe tool use and verification in the pull request | List a tool as author or `Co-authored-by` |
| Submit issues, reports and pull requests yourself | Let a tool submit issues, reports or pull requests on its own |
| Report vulnerabilities privately | Post tool-found vulnerabilities in public issues |
