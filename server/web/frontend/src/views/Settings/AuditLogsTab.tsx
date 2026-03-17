// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React, { useState, useEffect, useCallback } from "react";
import {
  Alert,
  Button,
  DescriptionList,
  DescriptionListDescription,
  DescriptionListGroup,
  DescriptionListTerm,
  Flex,
  FlexItem,
  Modal,
  ModalVariant,
  ModalHeader,
  ModalBody,
  ModalFooter,
  Pagination,
  Spinner,
  Content,
  TextInput,
  Toolbar,
  ToolbarContent,
  ToolbarItem,
} from "@patternfly/react-core";
import { Table, Thead, Tr, Th, Tbody, Td } from "@patternfly/react-table";
import DownloadIcon from "@patternfly/react-icons/dist/esm/icons/download-icon";
import ExpandIcon from "@patternfly/react-icons/dist/esm/icons/expand-icon";

import { hasPermission } from "../../apiClient/permissions";
import {
  fetchAuditLogs,
  exportAuditLogs,
  AuditLog,
  AuditLogListParams,
} from "../../apiClient/auditLogsApi";

// ─── Tamper event detail types ───────────────────────────────────────────────

interface TamperProcess {
  pid: number;
  comm: string;
  user: string;
}

interface TamperDetailsData {
  file?: string;
  node?: string;
  processes?: TamperProcess[];
}

function parseTamperDetails(raw: string): TamperDetailsData | null {
  try {
    const d = JSON.parse(raw);
    if (d && typeof d === "object" && ("file" in d || "processes" in d)) {
      return d as TamperDetailsData;
    }
  } catch {
    // not JSON
  }
  return null;
}

/** One-line summary shown in the truncated table cell. */
function tamperDetailsSummary(d: TamperDetailsData): string {
  if (!d.processes || d.processes.length === 0) {
    return "no process identified";
  }
  const parts = d.processes.map((p) => `${p.comm} (${p.user}, pid ${p.pid})`);
  if (parts.length <= 2) return parts.join(", ");
  return `${parts[0]}, ${parts[1]} +${parts.length - 2} more`;
}

/** Structured breakdown shown in the expand modal. */
const TamperDetailsView: React.FC<{ data: TamperDetailsData }> = ({ data }) => (
  <div>
    <DescriptionList isCompact isHorizontal style={{ marginBottom: 16 }}>
      {data.file && (
        <DescriptionListGroup>
          <DescriptionListTerm>File</DescriptionListTerm>
          <DescriptionListDescription>
            <code style={{ wordBreak: "break-all" }}>{data.file}</code>
          </DescriptionListDescription>
        </DescriptionListGroup>
      )}
      {data.node && (
        <DescriptionListGroup>
          <DescriptionListTerm>Node</DescriptionListTerm>
          <DescriptionListDescription>{data.node}</DescriptionListDescription>
        </DescriptionListGroup>
      )}
    </DescriptionList>

    <strong>Processes with file open at detection time</strong>
    {!data.processes || data.processes.length === 0 ? (
      <p style={{ color: "var(--pf-v5-global--Color--200)", marginTop: 8 }}>
        No process identified — the modifying process had already exited.
      </p>
    ) : (
      <table
        style={{
          width: "100%",
          marginTop: 8,
          borderCollapse: "collapse",
          fontSize: "var(--pf-v5-global--FontSize--sm)",
        }}
      >
        <thead>
          <tr>
            {["PID", "Process", "User"].map((h) => (
              <th
                key={h}
                style={{
                  textAlign: "left",
                  padding: "4px 8px",
                  borderBottom: "1px solid var(--pf-v5-global--BorderColor--100)",
                  color: "var(--pf-v5-global--Color--200)",
                  fontWeight: 600,
                }}
              >
                {h}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {data.processes.map((p) => (
            <tr key={p.pid}>
              <td style={{ padding: "4px 8px", fontFamily: "monospace" }}>{p.pid}</td>
              <td style={{ padding: "4px 8px", fontFamily: "monospace" }}>{p.comm}</td>
              <td style={{ padding: "4px 8px" }}>{p.user}</td>
            </tr>
          ))}
        </tbody>
      </table>
    )}
  </div>
);

// ─── Generic expand infrastructure ───────────────────────────────────────────

interface ExpandedField {
  label: string;
  /** Raw string shown for non-tamper fields. */
  content: string;
  /** Present when the field is a tamper event details JSON. */
  tamperData?: TamperDetailsData;
}

const TruncatedCell: React.FC<{
  content: string;
  maxWidth: number;
  label: string;
  onExpand: (field: ExpandedField) => void;
  tamperData?: TamperDetailsData;
}> = ({ content, maxWidth, label, onExpand, tamperData }) => {
  if (!content) {
    return <span>—</span>;
  }
  return (
    <span style={{ display: "flex", alignItems: "center", gap: 4 }}>
      <span
        style={{
          maxWidth,
          display: "inline-block",
          overflow: "hidden",
          textOverflow: "ellipsis",
          whiteSpace: "nowrap",
          verticalAlign: "middle",
        }}
        title={content}
      >
        {content}
      </span>
      <Button
        variant="plain"
        aria-label={`Expand ${label}`}
        onClick={() => onExpand({ label, content, tamperData })}
        style={{ padding: 0, minWidth: "auto", height: "auto", lineHeight: 1 }}
      >
        <ExpandIcon
          style={{ fontSize: "0.75rem", color: "var(--pf-v5-global--Color--200)" }}
        />
      </Button>
    </span>
  );
};

// ─── Username formatting ──────────────────────────────────────────────────────

const formatUser = (entry: AuditLog): string =>
  entry.action === "tamper_detected" ? `node: ${entry.username}` : entry.username;

// ─── Main component ───────────────────────────────────────────────────────────

export const AuditLogsTab: React.FC = () => {
  const [logs, setLogs] = useState<AuditLog[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [page, setPage] = useState(1);
  const [perPage, setPerPage] = useState(25);
  const [total, setTotal] = useState(0);
  const [usernameFilter, setUsernameFilter] = useState("");
  const [exporting, setExporting] = useState(false);
  const [expandedField, setExpandedField] = useState<ExpandedField | null>(null);

  const canExport = hasPermission("audit_log:export");

  const reload = useCallback(() => {
    setLoading(true);
    setError(null);
    const params: AuditLogListParams = { page, per_page: perPage };
    if (usernameFilter.trim()) {
      params.username = usernameFilter.trim();
    }
    fetchAuditLogs(params)
      .then((resp) => {
        setLogs(resp.items || []);
        setTotal(resp.total);
      })
      .catch((e) => setError(e.message))
      .finally(() => setLoading(false));
  }, [page, perPage, usernameFilter]);

  useEffect(() => {
    reload();
  }, [reload]);

  const handleExport = async (format: "csv" | "json") => {
    setExporting(true);
    setError(null);
    try {
      const params: AuditLogListParams = {};
      if (usernameFilter.trim()) params.username = usernameFilter.trim();
      await exportAuditLogs(format, params);
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "Export failed");
    } finally {
      setExporting(false);
    }
  };

  const handleFilterKeyPress = (event: React.KeyboardEvent) => {
    if (event.key === "Enter") {
      setPage(1);
      reload();
    }
  };

  const formatTimestamp = (ts: string) => {
    try {
      return new Date(ts).toLocaleString();
    } catch {
      return ts;
    }
  };

  return (
    <>
      {error && (
        <Alert variant="danger" title={error} isInline style={{ marginBottom: 16 }} />
      )}

      <Toolbar>
        <ToolbarContent>
          <ToolbarItem>
            <TextInput
              type="text"
              aria-label="Filter by username"
              placeholder="Filter by username..."
              value={usernameFilter}
              onChange={(_ev, v) => setUsernameFilter(v)}
              onKeyUp={handleFilterKeyPress}
            />
          </ToolbarItem>
          {canExport && (
            <ToolbarItem align={{ default: "alignRight" }}>
              <Flex>
                <FlexItem>
                  <Button
                    variant="secondary"
                    icon={<DownloadIcon />}
                    onClick={() => handleExport("csv")}
                    isDisabled={exporting}
                    isLoading={exporting}
                  >
                    Export CSV
                  </Button>
                </FlexItem>
                <FlexItem>
                  <Button
                    variant="secondary"
                    icon={<DownloadIcon />}
                    onClick={() => handleExport("json")}
                    isDisabled={exporting}
                    isLoading={exporting}
                  >
                    Export JSON
                  </Button>
                </FlexItem>
              </Flex>
            </ToolbarItem>
          )}
        </ToolbarContent>
      </Toolbar>

      {loading ? (
        <Spinner size="lg" />
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
                const tamperData =
                  entry.action === "tamper_detected"
                    ? parseTamperDetails(entry.details)
                    : null;
                const detailsDisplay = tamperData
                  ? tamperDetailsSummary(tamperData)
                  : entry.details;

                return (
                  <Tr key={entry.id}>
                    <Td>{formatTimestamp(entry.created_at)}</Td>
                    <Td>
                      <TruncatedCell
                        content={formatUser(entry)}
                        maxWidth={160}
                        label="User"
                        onExpand={setExpandedField}
                      />
                    </Td>
                    <Td>
                      <TruncatedCell
                        content={entry.action}
                        maxWidth={120}
                        label="Action"
                        onExpand={setExpandedField}
                      />
                    </Td>
                    <Td>
                      <TruncatedCell
                        content={entry.resource_type}
                        maxWidth={120}
                        label="Resource Type"
                        onExpand={setExpandedField}
                      />
                    </Td>
                    <Td>
                      <TruncatedCell
                        content={entry.resource_id}
                        maxWidth={150}
                        label="Resource ID"
                        onExpand={setExpandedField}
                      />
                    </Td>
                    <Td>
                      <TruncatedCell
                        content={detailsDisplay}
                        maxWidth={250}
                        label="Details"
                        onExpand={setExpandedField}
                        tamperData={tamperData ?? undefined}
                      />
                    </Td>
                    <Td>
                      <TruncatedCell
                        content={entry.ip_address}
                        maxWidth={140}
                        label="IP Address"
                        onExpand={setExpandedField}
                      />
                    </Td>
                  </Tr>
                );
              })}
              {logs.length === 0 && (
                <Tr>
                  <Td colSpan={7}>No audit log entries found.</Td>
                </Tr>
              )}
            </Tbody>
          </Table>

          <Pagination
            itemCount={total}
            page={page}
            perPage={perPage}
            onSetPage={(_ev, p) => setPage(p)}
            onPerPageSelect={(_ev, pp) => {
              setPerPage(pp);
              setPage(1);
            }}
            variant="bottom"
          />
        </>
      )}

      <Modal
        variant={ModalVariant.small}
        isOpen={expandedField !== null}
        onClose={() => setExpandedField(null)}
      >
        <ModalHeader title={expandedField?.label ?? ""} />
        <ModalBody>
          {expandedField?.tamperData ? (
            <TamperDetailsView data={expandedField.tamperData} />
          ) : (
            <Content>
              <pre style={{ whiteSpace: "pre-wrap", wordBreak: "break-all", margin: 0 }}>
                {expandedField?.content}
              </pre>
            </Content>
          )}
        </ModalBody>
        <ModalFooter>
          <Button key="close" variant="primary" onClick={() => setExpandedField(null)}>
            Close
          </Button>
        </ModalFooter>
      </Modal>
    </>
  );
};
