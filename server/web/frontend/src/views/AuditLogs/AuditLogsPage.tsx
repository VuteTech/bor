// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React, { useState, useEffect, useCallback, useRef } from "react";
import {
  Alert,
  Button,
  DescriptionList,
  DescriptionListDescription,
  DescriptionListGroup,
  DescriptionListTerm,
  Drawer,
  DrawerActions,
  DrawerCloseButton,
  DrawerContent,
  DrawerContentBody,
  DrawerHead,
  DrawerPanelBody,
  DrawerPanelContent,
  Flex,
  FlexItem,
  Menu,
  MenuContent,
  MenuItem,
  MenuList,
  MenuToggle,
  PageSection,
  Pagination,
  Popper,
  Spinner,
  TextInput,
  Toolbar,
  ToolbarContent,
  ToolbarGroup,
  ToolbarItem,
} from "@patternfly/react-core";
import { Table, Thead, Tr, Th, Tbody, Td } from "@patternfly/react-table";
import DownloadIcon from "@patternfly/react-icons/dist/esm/icons/download-icon";
import SyncIcon from "@patternfly/react-icons/dist/esm/icons/sync-icon";
import TimesIcon from "@patternfly/react-icons/dist/esm/icons/times-icon";

import { hasPermission } from "../../apiClient/permissions";
import {
  fetchAuditLogs,
  exportAuditLogs,
  AuditLog,
  AuditLogListParams,
} from "../../apiClient/auditLogsApi";

// ─── Known filter values ──────────────────────────────────────────────────────

const KNOWN_ACTIONS = ["create", "update", "delete", "tamper_detected"];
const KNOWN_RESOURCE_TYPES = [
  "policies", "nodes", "node-groups", "users", "roles",
  "user-groups", "policy-bindings", "managed_file", "settings",
];

// ─── Color definitions ────────────────────────────────────────────────────────

interface ColorStyle { bg: string; color: string }

const ACTION_COLORS: Record<string, ColorStyle> = {
  create:          { bg: "#3e8635", color: "#fff" },
  update:          { bg: "#0066cc", color: "#fff" },
  delete:          { bg: "#c9190b", color: "#fff" },
  tamper_detected: { bg: "#f0ab00", color: "#1f1f1f" },
};
const DEFAULT_ACTION_COLOR: ColorStyle = { bg: "#6a6e73", color: "#fff" };

const FILTER_TYPE_COLORS: Record<FilterType, ColorStyle> = {
  action:        { bg: "#6753ac", color: "#fff" },
  resource_type: { bg: "#009596", color: "#fff" },
  username:      { bg: "#4f5d75", color: "#fff" },
};

function chipColor(type: FilterType, value: string): ColorStyle {
  if (type === "action") return ACTION_COLORS[value] ?? DEFAULT_ACTION_COLOR;
  return FILTER_TYPE_COLORS[type];
}

// ─── Filter chip types ────────────────────────────────────────────────────────

type FilterType = "action" | "resource_type" | "username";

interface FilterChip { id: string; type: FilterType; value: string }

let _chipSeq = 0;
function nextChipId() { return String(++_chipSeq); }

// ─── Tamper event types ───────────────────────────────────────────────────────

interface TamperProcess { pid: number; comm: string; user: string }
interface TamperDetailsData { file?: string; node?: string; processes?: TamperProcess[] }

function parseTamperDetails(raw: string): TamperDetailsData | null {
  try {
    const d = JSON.parse(raw);
    if (d && typeof d === "object" && ("file" in d || "processes" in d)) return d;
  } catch { /* not JSON */ }
  return null;
}

function tamperDetailsSummary(d: TamperDetailsData): string {
  if (!d.processes || d.processes.length === 0) return "no process identified";
  const parts = d.processes.map((p) => `${p.comm} (${p.user})`);
  if (parts.length <= 2) return parts.join(", ");
  return `${parts[0]}, ${parts[1]} +${parts.length - 2} more`;
}

// ─── Structured body details ──────────────────────────────────────────────────

/** Parses the stored audit details as a JSON object. Returns null for tamper events
 *  or non-JSON details strings. */
function parseBodyDetails(raw: string): Record<string, unknown> | null {
  if (!raw) return null;
  try {
    const d = JSON.parse(raw);
    if (d && typeof d === "object" && !Array.isArray(d)) {
      // Exclude tamper event payloads (they have file/processes keys).
      if ("file" in d || "processes" in d) return null;
      return d as Record<string, unknown>;
    }
  } catch { /* not JSON */ }
  return null;
}

/** Returns the resource name from a parsed body details object, if present. */
function extractResourceName(details: Record<string, unknown> | null): string | null {
  if (!details) return null;
  for (const k of ["name", "display_name", "username", "email"]) {
    if (typeof details[k] === "string" && details[k]) return details[k] as string;
  }
  return null;
}

/** Formats a camelCase/snake_case key for human display. */
function formatKey(key: string): string {
  return key
    .replace(/_/g, " ")
    .replace(/([a-z])([A-Z])/g, "$1 $2")
    .replace(/^./, (c) => c.toUpperCase());
}

/** Renders a single JSON value as a string for display. */
function renderValue(val: unknown): string {
  if (val === null || val === undefined) return "—";
  if (typeof val === "boolean") return val ? "true" : "false";
  if (typeof val === "string") return val || "—";
  if (typeof val === "number") return String(val);
  return JSON.stringify(val);
}

// ─── Shared small components ──────────────────────────────────────────────────

const ActionBadge: React.FC<{ action: string }> = ({ action }) => {
  const { bg, color } = ACTION_COLORS[action] ?? DEFAULT_ACTION_COLOR;
  return (
    <span style={{
      background: bg, color, borderRadius: 9999,
      padding: "2px 10px", fontSize: "0.75rem", fontWeight: 600,
      whiteSpace: "nowrap", display: "inline-block",
    }}>
      {action}
    </span>
  );
};

const FilterPill: React.FC<{ chip: FilterChip; onRemove: () => void }> = ({ chip, onRemove }) => {
  const { bg, color } = chipColor(chip.type, chip.value);
  return (
    <span style={{
      display: "inline-flex", alignItems: "center", gap: 4,
      background: bg, color, borderRadius: 9999,
      padding: "2px 6px 2px 10px", fontSize: "0.8rem", fontWeight: 500,
    }}>
      {chip.value}
      <button
        aria-label={`Remove filter ${chip.value}`}
        onClick={onRemove}
        style={{ background: "none", border: "none", cursor: "pointer", color: "inherit", padding: 0, display: "flex", alignItems: "center", opacity: 0.8, lineHeight: 1 }}
      >
        <TimesIcon style={{ fontSize: "0.7rem" }} />
      </button>
    </span>
  );
};

const FilterDropdown: React.FC<{
  label: string; options: string[]; activeValues: string[]; onAdd: (v: string) => void;
}> = ({ label, options, activeValues, onAdd }) => {
  const [isOpen, setIsOpen] = useState(false);
  const toggleRef = useRef<HTMLButtonElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!isOpen) return;
    const handler = (e: MouseEvent) => {
      if (
        toggleRef.current && !toggleRef.current.contains(e.target as Node) &&
        menuRef.current && !menuRef.current.contains(e.target as Node)
      ) setIsOpen(false);
    };
    document.addEventListener("mousedown", handler);
    return () => document.removeEventListener("mousedown", handler);
  }, [isOpen]);

  const available = options.filter((o) => !activeValues.includes(o));

  return (
    <Popper
      trigger={
        <MenuToggle ref={toggleRef} onClick={() => setIsOpen((v) => !v)} isExpanded={isOpen}>
          {label}
        </MenuToggle>
      }
      popper={
        <Menu ref={menuRef} onSelect={(_ev, itemId) => { if (typeof itemId === "string") onAdd(itemId); }}>
          <MenuContent>
            <MenuList>
              {available.length === 0
                ? <MenuItem isDisabled key="none">All values active</MenuItem>
                : available.map((opt) => <MenuItem key={opt} itemId={opt}>{opt}</MenuItem>)
              }
            </MenuList>
          </MenuContent>
        </Menu>
      }
      isVisible={isOpen}
      enableFlip
    />
  );
};

// ─── Detail panel section label ───────────────────────────────────────────────

const SectionLabel: React.FC<{ children: React.ReactNode }> = ({ children }) => (
  <div style={{
    fontSize: "0.6875rem", fontWeight: 700, textTransform: "uppercase",
    letterSpacing: "0.1em", color: "#6a6e73",
    marginTop: 20, marginBottom: 8,
    paddingBottom: 4, borderBottom: "1px solid var(--pf-v5-global--BorderColor--100)",
  }}>
    {children}
  </div>
);

// ─── Detail panel content ─────────────────────────────────────────────────────

const EntryDetailPanel: React.FC<{ entry: AuditLog; onClose: () => void }> = ({ entry, onClose }) => {
  const { bg: accentBg } = ACTION_COLORS[entry.action] ?? DEFAULT_ACTION_COLOR;
  const tamperData = entry.action === "tamper_detected" ? parseTamperDetails(entry.details) : null;
  const bodyDetails = tamperData ? null : parseBodyDetails(entry.details);
  const resourceName = extractResourceName(bodyDetails);

  const formattedTimestamp = (() => {
    try {
      const d = new Date(entry.created_at);
      return d.toLocaleDateString(undefined, { year: "numeric", month: "long", day: "numeric" })
        + " · "
        + d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit", second: "2-digit" });
    } catch { return entry.created_at; }
  })();

  const displayUser = entry.action === "tamper_detected"
    ? `node: ${entry.username}`
    : entry.username;

  return (
    <DrawerPanelContent style={{ borderLeft: `3px solid ${accentBg}` }}>
      <DrawerHead>
        <div>
          <div style={{ display: "flex", alignItems: "center", gap: 10, marginBottom: 4 }}>
            <ActionBadge action={entry.action} />
          </div>
          <div style={{ fontSize: "0.8125rem", color: "#6a6e73", marginTop: 4 }}>
            {formattedTimestamp}
          </div>
        </div>
        <DrawerActions>
          <DrawerCloseButton onClick={onClose} />
        </DrawerActions>
      </DrawerHead>

      <DrawerPanelBody>

        {/* ── Identity ── */}
        <SectionLabel>Identity</SectionLabel>
        <DescriptionList isCompact>
          <DescriptionListGroup>
            <DescriptionListTerm>{entry.action === "tamper_detected" ? "Node" : "User"}</DescriptionListTerm>
            <DescriptionListDescription>
              <code style={{ fontFamily: "monospace", fontSize: "0.875rem" }}>{displayUser}</code>
            </DescriptionListDescription>
          </DescriptionListGroup>
          {entry.ip_address && (
            <DescriptionListGroup>
              <DescriptionListTerm>IP Address</DescriptionListTerm>
              <DescriptionListDescription>
                <code style={{ fontFamily: "monospace", fontSize: "0.875rem" }}>{entry.ip_address}</code>
              </DescriptionListDescription>
            </DescriptionListGroup>
          )}
        </DescriptionList>

        {/* ── Event ── */}
        <SectionLabel>Event</SectionLabel>
        <DescriptionList isCompact>
          <DescriptionListGroup>
            <DescriptionListTerm>Action</DescriptionListTerm>
            <DescriptionListDescription><ActionBadge action={entry.action} /></DescriptionListDescription>
          </DescriptionListGroup>
          <DescriptionListGroup>
            <DescriptionListTerm>Resource Type</DescriptionListTerm>
            <DescriptionListDescription>{entry.resource_type || "—"}</DescriptionListDescription>
          </DescriptionListGroup>
          {resourceName && (
            <DescriptionListGroup>
              <DescriptionListTerm>Resource Name</DescriptionListTerm>
              <DescriptionListDescription>{resourceName}</DescriptionListDescription>
            </DescriptionListGroup>
          )}
          {entry.resource_id && (
            <DescriptionListGroup>
              <DescriptionListTerm>Resource ID</DescriptionListTerm>
              <DescriptionListDescription>
                <code style={{ fontFamily: "monospace", fontSize: "0.8125rem", wordBreak: "break-all" }}>
                  {entry.resource_id}
                </code>
              </DescriptionListDescription>
            </DescriptionListGroup>
          )}
        </DescriptionList>

        {/* ── Tamper: file & processes ── */}
        {tamperData && (
          <>
            {tamperData.file && (
              <>
                <SectionLabel>Tampered File</SectionLabel>
                <code style={{
                  display: "block", fontFamily: "monospace", fontSize: "0.8125rem",
                  wordBreak: "break-all", background: "var(--pf-v5-global--BackgroundColor--200)",
                  padding: "6px 10px", borderRadius: 4,
                }}>
                  {tamperData.file}
                </code>
              </>
            )}

            <SectionLabel>Processes at Detection Time</SectionLabel>
            {!tamperData.processes || tamperData.processes.length === 0 ? (
              <p style={{ color: "#6a6e73", fontSize: "0.875rem", margin: 0 }}>
                No process identified — the modifying process had already exited.
              </p>
            ) : (
              <table style={{ width: "100%", borderCollapse: "collapse", fontSize: "0.8125rem" }}>
                <thead>
                  <tr>
                    {["PID", "Process", "User"].map((h) => (
                      <th key={h} style={{
                        textAlign: "left", padding: "4px 8px",
                        borderBottom: "1px solid var(--pf-v5-global--BorderColor--100)",
                        color: "#6a6e73", fontWeight: 600, fontSize: "0.75rem",
                      }}>
                        {h}
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {tamperData.processes.map((p) => (
                    <tr key={p.pid}>
                      <td style={{ padding: "5px 8px", fontFamily: "monospace" }}>{p.pid}</td>
                      <td style={{ padding: "5px 8px", fontFamily: "monospace" }}>{p.comm}</td>
                      <td style={{ padding: "5px 8px" }}>{p.user}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </>
        )}

        {/* ── Structured body details ── */}
        {bodyDetails && Object.keys(bodyDetails).length > 0 && (
          <>
            <SectionLabel>Changed Values</SectionLabel>
            <DescriptionList isCompact>
              {Object.entries(bodyDetails).map(([k, v]) => (
                <DescriptionListGroup key={k}>
                  <DescriptionListTerm>{formatKey(k)}</DescriptionListTerm>
                  <DescriptionListDescription>
                    {v === "[REDACTED]" ? (
                      <span style={{ color: "#6a6e73", fontStyle: "italic" }}>[redacted]</span>
                    ) : typeof v === "object" && v !== null ? (
                      <pre style={{
                        margin: 0, fontSize: "0.8125rem", fontFamily: "monospace",
                        whiteSpace: "pre-wrap", wordBreak: "break-all",
                        background: "var(--pf-v5-global--BackgroundColor--200)",
                        padding: "4px 8px", borderRadius: 4,
                      }}>
                        {JSON.stringify(v, null, 2)}
                      </pre>
                    ) : (
                      <span style={{ fontFamily: "monospace", fontSize: "0.875rem" }}>
                        {renderValue(v)}
                      </span>
                    )}
                  </DescriptionListDescription>
                </DescriptionListGroup>
              ))}
            </DescriptionList>
          </>
        )}

        {/* ── Fallback: plain-text details when body was not JSON ── */}
        {!tamperData && !bodyDetails && entry.details && (
          <>
            <SectionLabel>Details</SectionLabel>
            <pre style={{
              margin: 0, fontSize: "0.8125rem", fontFamily: "monospace",
              whiteSpace: "pre-wrap", wordBreak: "break-all",
              background: "var(--pf-v5-global--BackgroundColor--200)",
              padding: "8px 10px", borderRadius: 4,
              maxHeight: 300, overflowY: "auto",
            }}>
              {entry.details}
            </pre>
          </>
        )}

      </DrawerPanelBody>
    </DrawerPanelContent>
  );
};

// ─── Formatting helpers ───────────────────────────────────────────────────────

const formatUser = (entry: AuditLog) =>
  entry.action === "tamper_detected" ? `node: ${entry.username}` : entry.username;

const formatTimestamp = (ts: string) => {
  try { return new Date(ts).toLocaleString(); } catch { return ts; }
};

// ─── Main page ────────────────────────────────────────────────────────────────

export const AuditLogsPage: React.FC = () => {
  const [logs, setLogs] = useState<AuditLog[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [page, setPage] = useState(1);
  const [perPage, setPerPage] = useState(25);
  const [total, setTotal] = useState(0);
  const [filters, setFilters] = useState<FilterChip[]>([]);
  const [usernameInput, setUsernameInput] = useState("");
  const [exporting, setExporting] = useState(false);
  const [selectedEntry, setSelectedEntry] = useState<AuditLog | null>(null);

  const canExport = hasPermission("audit_log:export");

  const activeActions = filters.filter((f) => f.type === "action").map((f) => f.value);
  const activeResourceTypes = filters.filter((f) => f.type === "resource_type").map((f) => f.value);
  const activeUsernames = filters.filter((f) => f.type === "username").map((f) => f.value);

  const addFilter = (type: FilterType, value: string) => {
    if (filters.some((f) => f.type === type && f.value === value)) return;
    setFilters((prev) => [...prev, { id: nextChipId(), type, value }]);
    setPage(1);
  };

  const removeFilter = (id: string) => {
    setFilters((prev) => prev.filter((f) => f.id !== id));
    setPage(1);
  };

  const clearAllFilters = () => { setFilters([]); setPage(1); };

  const commitUsername = () => {
    const v = usernameInput.trim();
    if (v) { addFilter("username", v); setUsernameInput(""); }
  };

  const reload = useCallback(() => {
    setLoading(true);
    setError(null);
    const params: AuditLogListParams = { page, per_page: perPage };
    if (activeActions.length > 0) params.action = activeActions;
    if (activeResourceTypes.length > 0) params.resource_type = activeResourceTypes;
    if (activeUsernames.length > 0) params.username = activeUsernames[0];
    fetchAuditLogs(params)
      .then((resp) => { setLogs(resp.items || []); setTotal(resp.total); })
      .catch((e) => setError(e.message))
      .finally(() => setLoading(false));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [page, perPage, filters]);

  useEffect(() => { reload(); }, [reload]);

  const handleExport = async (format: "csv" | "json") => {
    setExporting(true);
    setError(null);
    try {
      const params: AuditLogListParams = {};
      if (activeActions.length > 0) params.action = activeActions;
      if (activeResourceTypes.length > 0) params.resource_type = activeResourceTypes;
      if (activeUsernames.length > 0) params.username = activeUsernames[0];
      await exportAuditLogs(format, params);
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "Export failed");
    } finally {
      setExporting(false);
    }
  };

  const panelContent = selectedEntry ? (
    <EntryDetailPanel entry={selectedEntry} onClose={() => setSelectedEntry(null)} />
  ) : <DrawerPanelContent />;

  return (
    <PageSection padding={{ default: "noPadding" }}>
      <Drawer isExpanded={selectedEntry !== null} position="right">
        <DrawerContent panelContent={panelContent}>
          <DrawerContentBody>
            <div style={{ padding: "1rem 1.5rem" }}>

              {error && <Alert variant="danger" title={error} isInline style={{ marginBottom: 16 }} />}

              {/* ── Toolbar ── */}
              <Toolbar clearAllFilters={filters.length > 0 ? clearAllFilters : undefined}>
                <ToolbarContent>
                  <ToolbarItem>
                    <FilterDropdown label="Action" options={KNOWN_ACTIONS} activeValues={activeActions} onAdd={(v) => addFilter("action", v)} />
                  </ToolbarItem>
                  <ToolbarItem>
                    <FilterDropdown label="Resource Type" options={KNOWN_RESOURCE_TYPES} activeValues={activeResourceTypes} onAdd={(v) => addFilter("resource_type", v)} />
                  </ToolbarItem>
                  <ToolbarItem>
                    <Flex spaceItems={{ default: "spaceItemsSm" }}>
                      <FlexItem>
                        <TextInput type="text" aria-label="Filter by username" placeholder="Username…" value={usernameInput}
                          onChange={(_ev, v) => setUsernameInput(v)}
                          onKeyUp={(e) => { if (e.key === "Enter") commitUsername(); }}
                          style={{ width: 160 }}
                        />
                      </FlexItem>
                      <FlexItem>
                        <Button variant="control" onClick={commitUsername}>Add</Button>
                      </FlexItem>
                    </Flex>
                  </ToolbarItem>
                  <ToolbarItem>
                    <Button variant="plain" aria-label="Refresh" onClick={reload} isDisabled={loading}>
                      <SyncIcon />
                    </Button>
                  </ToolbarItem>
                  {canExport && (
                    <ToolbarGroup align={{ default: "alignRight" }}>
                      <ToolbarItem>
                        <Button variant="secondary" icon={<DownloadIcon />} onClick={() => handleExport("csv")} isDisabled={exporting} isLoading={exporting}>
                          Export CSV
                        </Button>
                      </ToolbarItem>
                      <ToolbarItem>
                        <Button variant="secondary" icon={<DownloadIcon />} onClick={() => handleExport("json")} isDisabled={exporting} isLoading={exporting}>
                          Export JSON
                        </Button>
                      </ToolbarItem>
                    </ToolbarGroup>
                  )}
                </ToolbarContent>

                {filters.length > 0 && (
                  <ToolbarContent>
                    <ToolbarItem>
                      <Flex spaceItems={{ default: "spaceItemsSm" }} flexWrap={{ default: "wrap" }}>
                        {filters.map((chip) => (
                          <FlexItem key={chip.id}>
                            <FilterPill chip={chip} onRemove={() => removeFilter(chip.id)} />
                          </FlexItem>
                        ))}
                        <FlexItem>
                          <Button variant="link" isInline onClick={clearAllFilters} style={{ fontSize: "0.8rem" }}>
                            Clear all
                          </Button>
                        </FlexItem>
                      </Flex>
                    </ToolbarItem>
                  </ToolbarContent>
                )}
              </Toolbar>

              {/* ── Table ── */}
              {loading ? (
                <Spinner size="lg" style={{ marginTop: 32 }} />
              ) : (
                <>
                  <Table aria-label="Audit logs table" variant="compact">
                    <Thead>
                      <Tr>
                        <Th>Timestamp</Th>
                        <Th>User</Th>
                        <Th>Action</Th>
                        <Th>Resource Type</Th>
                        <Th>Resource ID</Th>
                        <Th>Details</Th>
                        <Th>IP Address</Th>
                      </Tr>
                    </Thead>
                    <Tbody>
                      {logs.map((entry) => {
                        const isSelected = selectedEntry?.id === entry.id;
                        const tamperData = entry.action === "tamper_detected"
                          ? parseTamperDetails(entry.details) : null;
                        const bodyDetails = tamperData ? null : parseBodyDetails(entry.details);
                        const resourceName = extractResourceName(bodyDetails);
                        const detailsSummary = tamperData
                          ? tamperDetailsSummary(tamperData)
                          : resourceName
                          ? resourceName
                          : bodyDetails
                          ? `${Object.keys(bodyDetails).length} field(s) changed`
                          : entry.details;

                        return (
                          <Tr
                            key={entry.id}
                            onClick={() => setSelectedEntry(isSelected ? null : entry)}
                            style={{
                              cursor: "pointer",
                              background: isSelected
                                ? "var(--pf-v5-global--BackgroundColor--200)"
                                : undefined,
                            }}
                            isHoverable
                            isRowSelected={isSelected}
                          >
                            <Td style={{ whiteSpace: "nowrap" }}>{formatTimestamp(entry.created_at)}</Td>
                            <Td>
                              <span style={{ display: "inline-block", maxWidth: 160, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", verticalAlign: "middle" }} title={formatUser(entry)}>
                                {formatUser(entry) || "—"}
                              </span>
                            </Td>
                            <Td><ActionBadge action={entry.action} /></Td>
                            <Td>
                              <span style={{ display: "inline-block", maxWidth: 120, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }} title={entry.resource_type}>
                                {entry.resource_type || "—"}
                              </span>
                            </Td>
                            <Td>
                              <span style={{ display: "inline-block", maxWidth: 160, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", fontFamily: "monospace", fontSize: "0.8125rem" }} title={entry.resource_id}>
                                {entry.resource_id || "—"}
                              </span>
                            </Td>
                            <Td>
                              <span style={{ display: "inline-block", maxWidth: 240, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", color: "#6a6e73", fontSize: "0.875rem" }} title={detailsSummary}>
                                {detailsSummary || "—"}
                              </span>
                            </Td>
                            <Td>
                              <span style={{ fontFamily: "monospace", fontSize: "0.8125rem" }}>
                                {entry.ip_address || "—"}
                              </span>
                            </Td>
                          </Tr>
                        );
                      })}
                      {logs.length === 0 && (
                        <Tr><Td colSpan={7}>No audit log entries found.</Td></Tr>
                      )}
                    </Tbody>
                  </Table>

                  <Pagination
                    itemCount={total}
                    page={page}
                    perPage={perPage}
                    onSetPage={(_ev, p) => setPage(p)}
                    onPerPageSelect={(_ev, pp) => { setPerPage(pp); setPage(1); }}
                    variant="bottom"
                  />
                </>
              )}

            </div>
          </DrawerContentBody>
        </DrawerContent>
      </Drawer>
    </PageSection>
  );
};
