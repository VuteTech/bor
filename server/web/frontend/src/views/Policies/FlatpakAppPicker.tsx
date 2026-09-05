// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

/**
 * FlatpakAppPicker — search the server-indexed Flatpak catalog and add apps
 * to a policy, or add an app by ID for remotes the server does not index.
 */

import React, { useCallback, useEffect, useId, useRef, useState } from "react";
import {
  Button,
  Checkbox,
  Flex,
  FlexItem,
  Label,
  MenuToggle,
  MenuToggleElement,
  SearchInput,
  Select,
  SelectList,
  SelectOption,
  Spinner,
  TextInput,
} from "@patternfly/react-core";
import { Table, Thead, Tr, Th, Tbody, Td } from "@patternfly/react-table";
import CheckCircleIcon from "@patternfly/react-icons/dist/esm/icons/check-circle-icon";
import PlusCircleIcon from "@patternfly/react-icons/dist/esm/icons/plus-circle-icon";
import CubeIcon from "@patternfly/react-icons/dist/esm/icons/cube-icon";

import { LiveAlert } from "../../components/LiveAlert";
import {
  FlatpakCatalogApp,
  FlatpakCatalogRepo,
  flatpakCatalogIconUrl,
  isValidFlatpakAppId,
  searchFlatpakCatalog,
} from "../../apiClient/flatpakApi";

const PAGE_SIZE = 20;

export interface FlatpakAppPickerProps {
  repos: FlatpakCatalogRepo[];
  reposError: string | null;
  /** App IDs already in the policy (Add is disabled for them). */
  listedIds: Set<string>;
  onAddFromCatalog: (app: FlatpakCatalogApp) => void;
  onAddById: (appId: string) => void;
  isDisabled?: boolean;
}

/** Catalog icon with a neutral fallback when the server has none. */
const AppIcon: React.FC<{ repo: string; appId: string; hasIcon: boolean }> = ({ repo, appId, hasIcon }) => {
  const [failed, setFailed] = useState(false);
  if (!hasIcon || failed) {
    return (
      <span aria-hidden="true" style={{ display: "inline-flex", width: 32, height: 32, alignItems: "center", justifyContent: "center", color: "var(--pf-t--global--icon--color--subtle)" }}>
        <CubeIcon />
      </span>
    );
  }
  return (
    <img
      src={flatpakCatalogIconUrl(repo, appId)}
      alt=""
      aria-hidden="true"
      width={32}
      height={32}
      loading="lazy"
      onError={() => setFailed(true)}
      style={{ width: 32, height: 32, objectFit: "contain" }}
    />
  );
};

export const FlatpakAppPicker: React.FC<FlatpakAppPickerProps> = ({
  repos,
  reposError,
  listedIds,
  onAddFromCatalog,
  onAddById,
  isDisabled,
}) => {
  const idp = useId();
  const [search, setSearch] = useState("");
  const [debounced, setDebounced] = useState("");
  const [repo, setRepo] = useState("");
  const [repoOpen, setRepoOpen] = useState(false);
  const [verifiedOnly, setVerifiedOnly] = useState(false);
  const [includeConsole, setIncludeConsole] = useState(false);
  const [page, setPage] = useState(1);
  const [results, setResults] = useState<FlatpakCatalogApp[]>([]);
  const [total, setTotal] = useState(0);
  const [totalPages, setTotalPages] = useState(0);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [manualId, setManualId] = useState("");
  const [manualError, setManualError] = useState<string | null>(null);
  const requestSeq = useRef(0);

  useEffect(() => {
    const t = window.setTimeout(() => setDebounced(search.trim()), 300);
    return () => window.clearTimeout(t);
  }, [search]);

  useEffect(() => {
    setPage(1);
  }, [debounced, repo, verifiedOnly, includeConsole]);

  const enabledRepos = repos.filter((r) => r.catalog_enabled);
  const hasCatalog = enabledRepos.some((r) => r.app_count > 0);

  const runSearch = useCallback(async () => {
    if (!hasCatalog) return;
    const seq = ++requestSeq.current;
    setLoading(true);
    setError(null);
    try {
      const res = await searchFlatpakCatalog({
        search: debounced || undefined,
        repo: repo || undefined,
        kind: includeConsole ? "desktop-application,console-application" : "desktop-application",
        verified: verifiedOnly ? true : undefined,
        page,
        per_page: PAGE_SIZE,
      });
      if (seq !== requestSeq.current) return;
      setResults(res.items);
      setTotal(res.total);
      setTotalPages(res.total_pages);
    } catch (e: unknown) {
      if (seq !== requestSeq.current) return;
      setError(e instanceof Error ? e.message : "Catalog search failed");
    } finally {
      if (seq === requestSeq.current) setLoading(false);
    }
  }, [debounced, repo, verifiedOnly, includeConsole, page, hasCatalog]);

  useEffect(() => {
    void runSearch();
  }, [runSearch]);

  const handleManualAdd = () => {
    const id = manualId.trim();
    if (!isValidFlatpakAppId(id)) {
      setManualError("Enter a reverse-DNS application ID such as org.mozilla.firefox");
      return;
    }
    if (listedIds.has(id)) {
      setManualError(`${id} is already in this policy`);
      return;
    }
    setManualError(null);
    onAddById(id);
    setManualId("");
  };

  const repoLabel = repo ? (repos.find((r) => r.name === repo)?.title || repo) : "All repositories";
  const manualErrId = `${idp}-manual-error`;

  return (
    <div>
      <LiveAlert message={reposError} variant="warning" isInline style={{ marginBottom: 8 }} />
      {!reposError && !hasCatalog && (
        <LiveAlert
          message="No catalog is available yet"
          variant="info"
          isInline
          style={{ marginBottom: 8 }}
        >
          Add or refresh a repository under Settings → Flatpak repositories to search apps by name. You can still add apps by ID below.
        </LiveAlert>
      )}

      {hasCatalog && (
        <>
          <Flex spaceItems={{ default: "spaceItemsMd" }} alignItems={{ default: "alignItemsCenter" }} style={{ marginBottom: "0.5rem", rowGap: "0.5rem" }}>
            <FlexItem grow={{ default: "grow" }} style={{ minWidth: "16rem" }}>
              <SearchInput
                aria-label="Search the Flatpak catalog"
                placeholder="Search apps by name, ID or keyword"
                value={search}
                onChange={(_ev, v) => setSearch(v)}
                onClear={() => setSearch("")}
                isDisabled={isDisabled}
              />
            </FlexItem>
            <FlexItem>
              <Select
                id={`${idp}-repo`}
                isOpen={repoOpen}
                onOpenChange={setRepoOpen}
                selected={repo}
                onSelect={(_ev, val) => { setRepo(String(val ?? "")); setRepoOpen(false); }}
                toggle={(ref: React.Ref<MenuToggleElement>) => (
                  <MenuToggle ref={ref} onClick={() => setRepoOpen((v) => !v)} isExpanded={repoOpen} aria-label="Filter by repository" isDisabled={isDisabled}>
                    {repoLabel}
                  </MenuToggle>
                )}
              >
                <SelectList>
                  <SelectOption value="">All repositories</SelectOption>
                  {enabledRepos.map((r) => (
                    <SelectOption key={r.id} value={r.name} description={`${r.app_count.toLocaleString()} apps`}>{r.title || r.name}</SelectOption>
                  ))}
                </SelectList>
              </Select>
            </FlexItem>
            <FlexItem>
              <Checkbox id={`${idp}-verified`} label="Verified only" isChecked={verifiedOnly} onChange={(_ev, c) => setVerifiedOnly(c)} isDisabled={isDisabled} />
            </FlexItem>
            <FlexItem>
              <Checkbox id={`${idp}-console`} label="Include console apps" isChecked={includeConsole} onChange={(_ev, c) => setIncludeConsole(c)} isDisabled={isDisabled} />
            </FlexItem>
          </Flex>

          <LiveAlert message={error} variant="danger" isInline style={{ marginBottom: 8 }} />

          {loading && results.length === 0 ? (
            <Spinner size="md" aria-label="Loading" />
          ) : (
            <Table aria-label="Catalog search results" variant="compact">
              <Thead>
                <Tr>
                  <Th screenReaderText="Icon" />
                  <Th>Application</Th>
                  <Th>Summary</Th>
                  <Th>Version</Th>
                  <Th>Repository</Th>
                  <Th screenReaderText="Add" />
                </Tr>
              </Thead>
              <Tbody>
                {results.length === 0 && (
                  <Tr>
                    <Td colSpan={6}>
                      <span className="bor-text-secondary">
                        {debounced ? `No apps match "${debounced}".` : "Type to search the catalog."}
                      </span>
                    </Td>
                  </Tr>
                )}
                {results.map((app) => {
                  const listed = listedIds.has(app.app_id);
                  return (
                    <Tr key={`${app.repo_name}/${app.app_id}/${app.branch}`}>
                      <Td dataLabel="Icon"><AppIcon repo={app.repo_name} appId={app.app_id} hasIcon={app.has_icon} /></Td>
                      <Td dataLabel="Application">
                        <div>
                          {app.name || app.app_id}{" "}
                          {app.verified && (
                            <Label color="green" isCompact icon={<CheckCircleIcon />}>Verified</Label>
                          )}
                        </div>
                        <small style={{ fontFamily: "monospace" }} className="bor-text-secondary">{app.app_id}</small>
                      </Td>
                      <Td dataLabel="Summary">{app.summary}</Td>
                      <Td dataLabel="Version">{app.latest_version || "—"}</Td>
                      <Td dataLabel="Repository">{app.repo_name}{app.branch && app.branch !== "stable" ? ` (${app.branch})` : ""}</Td>
                      <Td dataLabel="Add" isActionCell>
                        <Button
                          variant="secondary"
                          size="sm"
                          icon={<PlusCircleIcon />}
                          onClick={() => onAddFromCatalog(app)}
                          isDisabled={isDisabled || listed}
                          aria-label={listed ? `${app.app_id} is already in the policy` : `Add ${app.name || app.app_id}`}
                        >
                          {listed ? "Added" : "Add"}
                        </Button>
                      </Td>
                    </Tr>
                  );
                })}
              </Tbody>
            </Table>
          )}

          {totalPages > 1 && (
            <Flex justifyContent={{ default: "justifyContentFlexEnd" }} alignItems={{ default: "alignItemsCenter" }} style={{ marginTop: "0.5rem" }}>
              <FlexItem><span className="bor-text-secondary">{total.toLocaleString()} results · page {page} of {totalPages}</span></FlexItem>
              <FlexItem>
                <Button variant="link" isInline onClick={() => setPage((p) => Math.max(1, p - 1))} isDisabled={page <= 1 || loading}>Previous</Button>
              </FlexItem>
              <FlexItem>
                <Button variant="link" isInline onClick={() => setPage((p) => Math.min(totalPages, p + 1))} isDisabled={page >= totalPages || loading}>Next</Button>
              </FlexItem>
            </Flex>
          )}
        </>
      )}

      <Flex alignItems={{ default: "alignItemsFlexStart" }} spaceItems={{ default: "spaceItemsSm" }} style={{ marginTop: "0.75rem" }}>
        <FlexItem grow={{ default: "grow" }} style={{ maxWidth: "28rem" }}>
          <TextInput
            id={`${idp}-manual`}
            value={manualId}
            onChange={(_ev, v) => { setManualId(v); setManualError(null); }}
            placeholder="Add by ID, e.g. com.example.App"
            aria-label="Application ID to add"
            isDisabled={isDisabled}
            style={{ fontFamily: "monospace" }}
            aria-invalid={manualError ? true : undefined}
            aria-describedby={manualError ? manualErrId : undefined}
            onKeyDown={(ev) => { if (ev.key === "Enter") { ev.preventDefault(); handleManualAdd(); } }}
          />
        </FlexItem>
        <FlexItem>
          <Button variant="secondary" icon={<PlusCircleIcon />} onClick={handleManualAdd} isDisabled={isDisabled || !manualId.trim()}>
            Add by ID
          </Button>
        </FlexItem>
      </Flex>
      <LiveAlert id={manualErrId} message={manualError} variant="danger" isInline style={{ marginTop: 8 }} />
    </div>
  );
};
