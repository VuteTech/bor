// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

/**
 * DeployAgentModal — the "Deploy agent" wizard (issue 143).
 *
 * One dialog, opened from the masthead download button or the Nodes page:
 * pick a platform and architecture, get a copy-paste bootstrap built from
 * this instance's own URL — CA trust, signed repository setup (apt/dnf/
 * zypper) or direct package download (Alpine/Arch), and an optional
 * enrollment step that generates a one-time token inline.
 *
 * Everything renders from GET /api/v1/agent-packages; when the server ships
 * without the package repository the provider reports `available: false`
 * and every entry point hides itself.
 */

import React, {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
} from "react";
import {
  Button,
  CodeBlock,
  CodeBlockCode,
  Modal,
  ModalBody,
  ModalHeader,
  ModalVariant,
  Tab,
  Tabs,
  TabTitleText,
  ToggleGroup,
  ToggleGroupItem,
  Tooltip,
} from "@patternfly/react-core";
import { Table, Thead, Tbody, Tr, Th, Td } from "@patternfly/react-table";
import CopyIcon from "@patternfly/react-icons/dist/esm/icons/copy-icon";
import DownloadIcon from "@patternfly/react-icons/dist/esm/icons/download-icon";
import {
  fetchAgentPackages,
  AgentPackagesResponse,
  AgentRepoFile,
} from "../apiClient/agentPackagesApi";
import {
  fetchNodeGroups,
  generateEnrollmentToken,
  NodeGroup,
  EnrollmentToken,
} from "../apiClient/nodeGroupsApi";
import { hasPermission } from "../apiClient/permissions";
import { LiveAlert } from "./LiveAlert";
import { SearchableSelect } from "./SearchableSelect";
import { useToast } from "./ToastHost";

/* ── Context: one modal instance, many openers ── */

interface DeployAgentContextValue {
  /** True when this server ships the agent package repository. */
  available: boolean;
  open: () => void;
}

const DeployAgentContext = createContext<DeployAgentContextValue>({
  available: false,
  open: () => {},
});

// useDeployAgent lets any screen open the shared wizard (and hide its own
// entry point when the repository is not installed).
export function useDeployAgent(): DeployAgentContextValue {
  return useContext(DeployAgentContext);
}

export const DeployAgentProvider: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const [info, setInfo] = useState<AgentPackagesResponse | null>(null);
  const [isOpen, setIsOpen] = useState(false);

  useEffect(() => {
    if (!hasPermission("node:view")) return;
    fetchAgentPackages()
      .then(setInfo)
      .catch(() => setInfo(null));
  }, []);

  const open = useCallback(() => setIsOpen(true), []);
  const value = useMemo(
    () => ({ available: Boolean(info?.repo_available), open }),
    [info, open]
  );

  return (
    <DeployAgentContext.Provider value={value}>
      {children}
      {info?.repo_available && info.manifest && (
        <DeployAgentModal info={info} isOpen={isOpen} onClose={() => setIsOpen(false)} />
      )}
    </DeployAgentContext.Provider>
  );
};

// DeployAgentMastheadButton — the global opener next to the theme toggles.
// Renders nothing when the repository is not installed.
export const DeployAgentMastheadButton: React.FC = () => {
  const { available, open } = useDeployAgent();
  if (!available) return null;
  return (
    <Tooltip content="Deploy agent" position="bottom">
      <Button
        variant="plain"
        aria-label="Deploy agent"
        onClick={open}
        style={{ color: "var(--bor--masthead--text)", padding: "0.375rem" }}
      >
        <DownloadIcon />
      </Button>
    </Tooltip>
  );
};

/* ── Platforms ── */

type PlatformID = "debian" | "rhel" | "suse" | "alpine" | "arch";
type Arch = "amd64" | "arm64" | "ppc64le";

const PLATFORMS: { id: PlatformID; label: string; format: AgentRepoFile["format"]; repo: boolean }[] = [
  { id: "debian", label: "Debian / Ubuntu", format: "deb", repo: true },
  { id: "rhel", label: "RHEL / Fedora", format: "rpm", repo: true },
  { id: "suse", label: "SUSE", format: "rpm", repo: true },
  { id: "alpine", label: "Alpine", format: "apk", repo: false },
  { id: "arch", label: "Arch Linux", format: "arch", repo: false },
];

const ARCHES: Arch[] = ["amd64", "arm64", "ppc64le"];

/* ── Snippet builders ──
 * Pure string builders from this instance's own URL. The first CA fetch has
 * to use -k: with the auto-generated certificate there is nothing to verify
 * against yet (comment in the snippet says so; every later fetch verifies).
 */

interface SnippetEnv {
  origin: string; // https://host[:port]
  host: string;
  port: string; // UI/enrollment port as browsers see it
  signed: boolean;
}

function caTrustStep(p: PlatformID, env: SnippetEnv): string {
  const dest: Record<PlatformID, [string, string]> = {
    debian: ["/usr/local/share/ca-certificates/bor-ca.crt", "sudo update-ca-certificates"],
    alpine: ["/usr/local/share/ca-certificates/bor-ca.crt", "sudo update-ca-certificates"],
    rhel: ["/etc/pki/ca-trust/source/anchors/bor-ca.crt", "sudo update-ca-trust"],
    suse: ["/etc/pki/trust/anchors/bor-ca.crt", "sudo update-ca-certificates"],
    arch: ["/etc/ca-certificates/trust-source/anchors/bor-ca.crt", "sudo update-ca-trust"],
  };
  const [path, refresh] = dest[p];
  return [
    "# 1) Trust this server's CA — skip when it already has a publicly trusted",
    "#    certificate. (-k: the CA is fetched before it can be verified; check",
    "#    the fingerprint out-of-band for high-security environments.)",
    `curl -fsSk ${env.origin}/agent/ca.crt | sudo tee ${path} >/dev/null`,
    refresh,
  ].join("\n");
}

function repoStep(p: PlatformID, env: SnippetEnv): string {
  switch (p) {
    case "debian":
      if (!env.signed) {
        return [
          "# 2) Add the Bor repository (UNSIGNED development build!)",
          `echo 'deb [trusted=yes] ${env.origin}/agent/deb ./' | sudo tee /etc/apt/sources.list.d/bor.list >/dev/null`,
          "sudo apt-get update && sudo apt-get install -y bor-agent",
        ].join("\n");
      }
      return [
        "# 2) Add the signed Bor repository served by this instance",
        "sudo install -d -m 755 /etc/apt/keyrings",
        `curl -fsS ${env.origin}/agent/repo-key.asc | sudo tee /etc/apt/keyrings/bor.asc >/dev/null`,
        `printf 'Types: deb\\nURIs: ${env.origin}/agent/deb/\\nSuites: ./\\nSigned-By: /etc/apt/keyrings/bor.asc\\n' | sudo tee /etc/apt/sources.list.d/bor.sources >/dev/null`,
        "sudo apt-get update && sudo apt-get install -y bor-agent",
      ].join("\n");
    case "rhel":
      return [
        "# 2) Add the Bor repository served by this instance",
        "cat <<EOF | sudo tee /etc/yum.repos.d/bor.repo >/dev/null",
        "[bor]",
        "name=Bor Agent",
        `baseurl=${env.origin}/agent/rpm/`,
        "enabled=1",
        "gpgcheck=0",
        `repo_gpgcheck=${env.signed ? 1 : 0}`,
        ...(env.signed ? [`gpgkey=${env.origin}/agent/repo-key.asc`] : []),
        "EOF",
        "sudo dnf install -y bor-agent",
      ].join("\n");
    case "suse":
      return [
        "# 2) Add the Bor repository served by this instance",
        `sudo zypper addrepo --refresh ${env.origin}/agent/rpm/ bor`,
        `sudo zypper --gpg-auto-import-keys install -y bor-agent`,
      ].join("\n");
    default:
      return "";
  }
}

function directStep(p: PlatformID, env: SnippetEnv, file: AgentRepoFile | undefined): string {
  if (!file) {
    return "# No package published for this architecture.";
  }
  const url = `${env.origin}/agent/${file.path}`;
  const name = file.path.split("/").pop() ?? file.path;
  if (p === "alpine") {
    return [
      "# 2) Download and install the package (the apk index is not signed with",
      "#    Alpine's scheme — delivery is protected by the TLS trust above)",
      `curl -fsSO ${url}`,
      `sudo apk add --allow-untrusted ./${name}`,
    ].join("\n");
  }
  return [
    "# 2) Download and install the package",
    `curl -fsSO ${url}`,
    `sudo pacman -U --noconfirm ./${name}`,
  ].join("\n");
}

function enrollStep(env: SnippetEnv, token: EnrollmentToken): string {
  return [
    "",
    "# 3) Point the agent at this server and enroll (token is single-use,",
    "#    valid 5 minutes). The CA trust from step 1 lets verification stay on.",
    `sudo sed -i 's|^\\( *address:\\).*|\\1 "${env.host}"|' /etc/bor/config.yaml`,
    `sudo sed -i 's|^\\( *enrollment_port:\\).*|\\1 ${env.port}|' /etc/bor/config.yaml`,
    "sudo sed -i 's|^\\( *insecure_skip_verify:\\).*|\\1 false|' /etc/bor/config.yaml",
    "# policy_port defaults to 8444 — adjust in /etc/bor/config.yaml if your",
    "# deployment exposes the mTLS policy port elsewhere.",
    "sudo install -m 600 /dev/null /etc/bor/enroll-token",
    `printf '%s' '${token.token}' | sudo tee /etc/bor/enroll-token >/dev/null`,
    "sudo bor-agent --token-file /etc/bor/enroll-token",
    "sudo rm -f /etc/bor/enroll-token",
    "sudo systemctl enable --now bor-agent",
  ].join("\n");
}

function formatSize(bytes: number): string {
  return bytes >= 1 << 20 ? `${(bytes / (1 << 20)).toFixed(1)} MB` : `${Math.ceil(bytes / 1024)} KB`;
}

/* ── The wizard ── */

const DeployAgentModal: React.FC<{
  info: AgentPackagesResponse;
  isOpen: boolean;
  onClose: () => void;
}> = ({ info, isOpen, onClose }) => {
  const manifest = info.manifest!;
  const { addToast } = useToast();

  const [platform, setPlatform] = useState<PlatformID>("debian");
  const [arch, setArch] = useState<Arch>("amd64");
  const [activeTab, setActiveTab] = useState<string | number>("install");
  const [groups, setGroups] = useState<NodeGroup[]>([]);
  const [groupId, setGroupId] = useState("");
  const [token, setToken] = useState<EnrollmentToken | null>(null);
  const [tokenError, setTokenError] = useState("");

  const canEnroll = hasPermission("node_group:view") && hasPermission("node_group:create");

  useEffect(() => {
    if (!isOpen || !canEnroll) return;
    fetchNodeGroups()
      .then(setGroups)
      .catch(() => setGroups([]));
  }, [isOpen, canEnroll]);

  // Fresh dialog on every open; a stale one-time token is worse than none.
  useEffect(() => {
    if (!isOpen) {
      setToken(null);
      setTokenError("");
    }
  }, [isOpen]);

  const env: SnippetEnv = useMemo(() => {
    const { protocol, hostname, port } = window.location;
    return {
      origin: window.location.origin,
      host: hostname,
      port: port || (protocol === "https:" ? "443" : "80"),
      signed: manifest.signed,
    };
  }, [manifest.signed]);

  const meta = PLATFORMS.find((p) => p.id === platform)!;
  const directFile = manifest.files.find((f) => f.format === meta.format && f.arch === arch);

  const snippet = useMemo(() => {
    const parts = [caTrustStep(platform, env)];
    parts.push(meta.repo ? repoStep(platform, env) : directStep(platform, env, directFile));
    if (token) parts.push(enrollStep(env, token));
    return parts.join("\n");
  }, [platform, env, meta.repo, directFile, token]);

  const copySnippet = () => {
    navigator.clipboard
      .writeText(snippet)
      .then(() => addToast({ variant: "success", title: "Install script copied to clipboard" }))
      .catch(() => addToast({ variant: "danger", title: "Copying to the clipboard failed" }));
  };

  const generateToken = () => {
    setTokenError("");
    generateEnrollmentToken(groupId)
      .then(setToken)
      .catch((err: Error) => setTokenError(err.message || "Failed to generate a token"));
  };

  const archFiles = manifest.files.filter((f) => f.arch === arch);

  return (
    <Modal variant={ModalVariant.large} isOpen={isOpen} onClose={onClose} aria-label="Deploy agent">
      <ModalHeader
        title="Deploy agent"
        description={`Install bor-agent ${manifest.version} from this server — packages and repositories are served by this instance, no internet access required on the nodes.`}
      />
      <ModalBody>
        <LiveAlert
          variant="warning"
          isInline
          message={
            !info.version_match
              ? `The packaged agent version (${manifest.version}) differs from this server (${info.server_version}).`
              : !manifest.signed
                ? "This repository is UNSIGNED (development build) — the snippets fall back to trusted=yes / gpgcheck=0."
                : null
          }
        />

        <div style={{ display: "flex", flexWrap: "wrap", gap: "1rem", margin: "1rem 0" }}>
          <ToggleGroup aria-label="Platform">
            {PLATFORMS.map((p) => (
              <ToggleGroupItem
                key={p.id}
                text={p.label}
                isSelected={platform === p.id}
                onChange={() => setPlatform(p.id)}
              />
            ))}
          </ToggleGroup>
          <ToggleGroup aria-label="Architecture">
            {ARCHES.map((a) => (
              <ToggleGroupItem
                key={a}
                text={a}
                isSelected={arch === a}
                onChange={() => setArch(a)}
              />
            ))}
          </ToggleGroup>
        </div>

        <Tabs
          activeKey={activeTab}
          onSelect={(_ev, key) => setActiveTab(key)}
          aria-label="Deployment method"
        >
          <Tab
            eventKey="install"
            title={<TabTitleText>{meta.repo ? "Set up repository" : "Install package"}</TabTitleText>}
            aria-label="Install instructions"
          >
            <div style={{ margin: "1rem 0" }}>
              {canEnroll && (
                <div style={{ display: "flex", flexWrap: "wrap", gap: "0.5rem", alignItems: "center", marginBottom: "1rem" }}>
                  <div style={{ minWidth: "16rem" }}>
                    <SearchableSelect
                      options={groups.map((g) => ({ value: g.id, label: g.name }))}
                      selected={groupId}
                      onSelect={setGroupId}
                      ariaLabel="Node group for enrollment"
                      placeholder="Enroll into node group…"
                    />
                  </div>
                  <Button variant="secondary" onClick={generateToken} isDisabled={!groupId}>
                    {token ? "Regenerate enrollment token" : "Add enrollment step"}
                  </Button>
                  {token && (
                    <span className="bor-text-secondary">
                      Token expires {new Date(token.expires_at).toLocaleTimeString()}
                    </span>
                  )}
                </div>
              )}
              <LiveAlert variant="danger" isInline message={tokenError || null} />

              <CodeBlock
                actions={
                  <Button variant="plain" aria-label="Copy install script" onClick={copySnippet}>
                    <CopyIcon />
                  </Button>
                }
              >
                <CodeBlockCode>{snippet}</CodeBlockCode>
              </CodeBlock>
              {!token && canEnroll && (
                <p className="bor-text-secondary" style={{ marginTop: "0.5rem" }}>
                  Pick a node group and add the enrollment step to get a single
                  script that installs, enrolls and starts the agent.
                </p>
              )}
            </div>
          </Tab>

          <Tab
            eventKey="downloads"
            title={<TabTitleText>Direct downloads</TabTitleText>}
            aria-label="Direct downloads"
          >
            <Table aria-label="Agent packages" variant="compact" style={{ marginTop: "1rem" }}>
              <Thead>
                <Tr>
                  <Th>Format</Th>
                  <Th>File</Th>
                  <Th>Size</Th>
                  <Th>SHA-256</Th>
                </Tr>
              </Thead>
              <Tbody>
                {archFiles.map((f) => (
                  <Tr key={f.path}>
                    <Td dataLabel="Format">{f.format}</Td>
                    <Td dataLabel="File">
                      <a href={`/agent/${f.path}`} download>
                        {f.path.split("/").pop()}
                      </a>
                    </Td>
                    <Td dataLabel="Size">{formatSize(f.size)}</Td>
                    <Td dataLabel="SHA-256">
                      <code style={{ fontSize: "var(--pf-t--global--font--size--sm)" }}>
                        {f.sha256.slice(0, 16)}…
                      </code>
                    </Td>
                  </Tr>
                ))}
              </Tbody>
            </Table>
            <p className="bor-text-secondary" style={{ marginTop: "0.5rem" }}>
              Showing {archFiles.length} packages for {arch}. Full checksums are
              in{" "}
              <a href="/agent/manifest.json" target="_blank" rel="noopener noreferrer">
                manifest.json
              </a>
              .
            </p>
          </Tab>
        </Tabs>

        <p className="bor-text-secondary" style={{ marginTop: "1rem" }}>
          Agent version {manifest.version}
          {manifest.signed ? ", repository signed" : ""} · built {manifest.generated_at}
        </p>
      </ModalBody>
    </Modal>
  );
};
