// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

/**
 * DiskEncryptionPolicyEditor - requirements, protectors (TPM2, Tang,
 * recovery key) and initramfs handling of a `DiskEncryption` policy.
 * Content is protojson (camelCase, enum names); see diskEncryptionModel.ts.
 * Tang servers are copied from the registry (Settings -> Tang servers) so the
 * policy stays self-contained.
 */

import React, { useEffect, useMemo, useState } from "react";
import {
  Button,
  Card,
  CardBody,
  CardTitle,
  Checkbox,
  ExpandableSection,
  Form,
  FormGroup,
  FormHelperText,
  Gallery,
  HelperText,
  HelperTextItem,
  Label,
  NumberInput,
  Radio,
  Select,
  SelectList,
  SelectOption,
  MenuToggle,
  MenuToggleElement,
  Switch,
  TextInput,
} from "@patternfly/react-core";
import TrashIcon from "@patternfly/react-icons/dist/esm/icons/trash-icon";

import { LiveAlert } from "../../components/LiveAlert";
import { ListEditor } from "./ListEditor";
import {
  fetchDiskEncryptionSummary,
  fetchTangServers,
  TangServer,
} from "../../apiClient/diskEncryptionApi";
import {
  DEFAULT_ROTATION_DAYS,
  DiskEncContent,
  DiskEncTangServer,
  parseDiskEncContent,
  PCR_PROFILE_OPTIONS,
  serializeDiskEncContent,
  validateDiskEncContent,
  VOLUME_SCOPE_OPTIONS,
} from "./diskEncryptionModel";

export interface DiskEncryptionPolicyEditorProps {
  contentRaw: string;
  onChange: (newRaw: string) => void;
  isDisabled?: boolean;
  onValidityChange?: (ok: boolean) => void;
}

export const DiskEncryptionPolicyEditor: React.FC<DiskEncryptionPolicyEditorProps> = ({
  contentRaw,
  onChange,
  isDisabled = false,
  onValidityChange,
}) => {
  const [content, setContent] = useState<DiskEncContent>(() => parseDiskEncContent(contentRaw));
  const [registry, setRegistry] = useState<TangServer[]>([]);
  const [registryError, setRegistryError] = useState<string | null>(null);
  const [escrowConfigured, setEscrowConfigured] = useState<boolean>(true);
  const [tangPickerOpen, setTangPickerOpen] = useState(false);
  const [advancedOpen, setAdvancedOpen] = useState(false);
  const [pcrsDraft, setPcrsDraft] = useState(() => content.tpm2.pcrs.join(", "));

  const validationError = useMemo(() => validateDiskEncContent(content), [content]);

  useEffect(() => {
    onValidityChange?.(validationError === null);
  }, [validationError, onValidityChange]);
  useEffect(() => () => onValidityChange?.(true), [onValidityChange]);

  useEffect(() => {
    let cancelled = false;
    fetchTangServers()
      .then((items) => {
        if (!cancelled) setRegistry(items);
      })
      .catch((err: Error) => {
        if (!cancelled) setRegistryError(err.message);
      });
    fetchDiskEncryptionSummary()
      .then((s) => {
        if (!cancelled) setEscrowConfigured(s.escrow_configured);
      })
      .catch(() => {
        /* the summary needs disk_encryption:view; keep the editor usable */
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const update = (mutate: (draft: DiskEncContent) => void) => {
    setContent((prev) => {
      const next: DiskEncContent = JSON.parse(JSON.stringify(prev));
      mutate(next);
      onChange(serializeDiskEncContent(next));
      return next;
    });
  };

  const addRegistryServer = (srv: TangServer) => {
    update((d) => {
      if (d.tang.servers.some((s) => s.url === srv.url)) return;
      const [preferred, ...accepted] = srv.trusted_thumbprints;
      d.tang.servers.push({
        url: srv.url,
        thumbprint: preferred ?? "",
        acceptedThumbprints: accepted,
        registryId: srv.id,
      });
    });
    setTangPickerOpen(false);
  };

  const removeTangServer = (index: number) => {
    update((d) => {
      d.tang.servers.splice(index, 1);
      if (d.tang.threshold > d.tang.servers.length) d.tang.threshold = d.tang.servers.length;
    });
  };

  const applyPcrsDraft = (value: string) => {
    setPcrsDraft(value);
    const pcrs = value
      .split(/[\s,]+/)
      .filter((v) => v !== "")
      .map((v) => Number(v))
      .filter((n) => Number.isInteger(n));
    update((d) => {
      d.tpm2.pcrs = pcrs;
    });
  };

  const availableRegistry = registry.filter(
    (r) => !content.tang.servers.some((s) => s.url === r.url),
  );

  return (
    <Form isHorizontal={false} onSubmit={(e) => e.preventDefault()}>
      {content.recovery.escrow && !escrowConfigured && (
        <LiveAlert
          id="disk-enc-no-kek"
          variant="danger"
          isInline
          message={
            "Recovery-key escrow is not configured on the server: set BOR_ESCROW_KEK_FILE. " +
            "Agents cannot escrow keys until it is, and this policy will report an error."
          }
        />
      )}

      {/* ── Requirements ─────────────────────────────────────────────── */}
      <Card>
        <CardTitle>Requirements</CardTitle>
        <CardBody>
          <FormGroup fieldId="de-require-encryption">
            <Switch
              id="de-require-encryption"
              label="Require encryption on the volumes in scope"
              isChecked={content.requireEncryption}
              onChange={(_e, v) => update((d) => (d.requireEncryption = v))}
              isDisabled={isDisabled}
            />
            <FormHelperText>
              <HelperText>
                <HelperTextItem>
                  An in-scope mount that is not backed by{" "}
                  <abbr title="Linux Unified Key Setup">LUKS</abbr> reports non-compliant. Bor never
                  encrypts a disk in place - reinstall or provision new machines encrypted.
                </HelperTextItem>
              </HelperText>
            </FormHelperText>
          </FormGroup>

          <FormGroup label="Volume scope" fieldId="de-volume-scope" role="radiogroup">
            {VOLUME_SCOPE_OPTIONS.map((opt) => (
              <Radio
                key={opt.value}
                id={`de-scope-${opt.value}`}
                name="de-volume-scope"
                label={opt.label}
                description={opt.description}
                isChecked={content.volumeScope === opt.value}
                onChange={() => update((d) => (d.volumeScope = opt.value))}
                isDisabled={isDisabled}
              />
            ))}
          </FormGroup>

          {content.volumeScope === "LUKS_VOLUME_SCOPE_MOUNTPOINTS" && (
            <FormGroup label="Mountpoints" fieldId="de-mountpoints">
              <ListEditor
                items={content.mountpoints}
                onRemove={(i) => update((d) => void d.mountpoints.splice(i, 1))}
                onAdd={() => update((d) => void d.mountpoints.push(""))}
                addLabel="Add a mountpoint"
                removeAriaLabel={(i) => `Remove mountpoint ${content.mountpoints[i] || i + 1}`}
                emptyText="No mountpoints yet - the policy needs at least one."
                isDisabled={isDisabled}
                renderItem={(m, i) => (
                  <TextInput
                    id={`de-mountpoint-${i}`}
                    value={m}
                    onChange={(_e, v) => update((d) => (d.mountpoints[i] = v))}
                    placeholder="/home"
                    aria-label={`Mountpoint ${i + 1}`}
                    isDisabled={isDisabled}
                  />
                )}
              />
            </FormGroup>
          )}

          <FormGroup fieldId="de-require-tpm2">
            <Switch
              id="de-require-tpm2"
              label="Require a TPM 2.0 chip"
              isChecked={content.requireTpm2}
              onChange={(_e, v) => update((d) => (d.requireTpm2 = v))}
              isDisabled={isDisabled}
            />
          </FormGroup>
          <FormGroup fieldId="de-require-sb">
            <Switch
              id="de-require-sb"
              label="Require Secure Boot enabled"
              isChecked={content.requireSecureBoot}
              onChange={(_e, v) => update((d) => (d.requireSecureBoot = v))}
              isDisabled={isDisabled}
            />
          </FormGroup>
        </CardBody>
      </Card>

      {/* ── Protectors ───────────────────────────────────────────────── */}
      <Gallery hasGutter minWidths={{ default: "100%", lg: "30%" }}>
        <Card>
          <CardTitle>
            <abbr title="Trusted Platform Module">TPM</abbr> 2.0 unlocking
          </CardTitle>
          <CardBody>
            <FormGroup fieldId="de-tpm2-enabled">
              <Switch
                id="de-tpm2-enabled"
                label="Unlock automatically with the TPM"
                isChecked={content.tpm2.enabled}
                onChange={(_e, v) => update((d) => (d.tpm2.enabled = v))}
                isDisabled={isDisabled}
              />
              <FormHelperText>
                <HelperText>
                  <HelperTextItem>
                    The disk unlocks without a prompt while the boot chain is intact
                    (systemd-cryptenroll on dracut systems).
                  </HelperTextItem>
                </HelperText>
              </FormHelperText>
            </FormGroup>
            {content.tpm2.enabled && (
              <>
                <FormGroup
                  label={
                    <>
                      <abbr title="Platform Configuration Register">PCR</abbr> profile
                    </>
                  }
                  fieldId="de-pcr-profile"
                  role="radiogroup"
                >
                  {PCR_PROFILE_OPTIONS.map((opt) => (
                    <Radio
                      key={opt.value}
                      id={`de-pcr-${opt.value}`}
                      name="de-pcr-profile"
                      label={opt.label}
                      description={opt.description}
                      isChecked={content.tpm2.pcrProfile === opt.value}
                      onChange={() => update((d) => (d.tpm2.pcrProfile = opt.value))}
                      isDisabled={isDisabled}
                    />
                  ))}
                </FormGroup>
                {content.tpm2.pcrProfile === "TPM_PCR_PROFILE_CUSTOM" && (
                  <>
                    <FormGroup label="PCRs" fieldId="de-pcrs">
                      <TextInput
                        id="de-pcrs"
                        value={pcrsDraft}
                        onChange={(_e, v) => applyPcrsDraft(v)}
                        placeholder="7, 14"
                        aria-label="Custom PCR list"
                        isDisabled={isDisabled}
                      />
                    </FormGroup>
                    <FormGroup fieldId="de-firmware-pcrs">
                      <Checkbox
                        id="de-firmware-pcrs"
                        label="Allow firmware PCRs (0 and 2)"
                        description="These change on firmware updates and make the enrollment brittle"
                        isChecked={content.tpm2.allowFirmwarePcrs}
                        onChange={(_e, v) => update((d) => (d.tpm2.allowFirmwarePcrs = v))}
                        isDisabled={isDisabled}
                      />
                    </FormGroup>
                  </>
                )}
                <FormGroup fieldId="de-auto-reseal">
                  <Switch
                    id="de-auto-reseal"
                    label="Re-enroll automatically after PCR changes"
                    isChecked={content.tpm2.autoReseal}
                    onChange={(_e, v) => update((d) => (d.tpm2.autoReseal = v))}
                    isDisabled={isDisabled}
                  />
                  <FormHelperText>
                    <HelperText>
                      <HelperTextItem>
                        After a firmware or Secure Boot update the next boot falls back to Tang or
                        the recovery key; once the system is up, the agent re-enrolls the TPM.
                      </HelperTextItem>
                    </HelperText>
                  </FormHelperText>
                </FormGroup>
              </>
            )}
          </CardBody>
        </Card>

        <Card>
          <CardTitle>Tang network unlocking</CardTitle>
          <CardBody>
            <FormGroup fieldId="de-tang-enabled">
              <Switch
                id="de-tang-enabled"
                label="Unlock automatically on the corporate network"
                isChecked={content.tang.enabled}
                onChange={(_e, v) => update((d) => (d.tang.enabled = v))}
                isDisabled={isDisabled}
              />
              <FormHelperText>
                <HelperText>
                  <HelperTextItem>
                    Clevis binds the volume to your Tang servers. A stolen device still unlocks
                    while it can reach them - combine with TPM2 or use it as the fallback for
                    firmware updates.
                  </HelperTextItem>
                </HelperText>
              </FormHelperText>
            </FormGroup>
            {content.tang.enabled && (
              <>
                {registryError && (
                  <LiveAlert
                    id="de-tang-registry-error"
                    variant="warning"
                    isInline
                    message={`The Tang server registry could not be loaded: ${registryError}`}
                  />
                )}
                <FormGroup label="Servers" fieldId="de-tang-servers">
                  {content.tang.servers.length === 0 && (
                    <p className="bor-text-secondary">
                      No servers yet - pick one from the registry below.
                    </p>
                  )}
                  {content.tang.servers.map((s: DiskEncTangServer, i) => (
                    <div
                      key={s.url || i}
                      style={{ display: "flex", gap: "0.5rem", marginBottom: "0.4rem", alignItems: "center" }}
                    >
                      <span>
                        <code>{s.url}</code>{" "}
                        <span className="bor-text-secondary">
                          key {s.thumbprint ? `${s.thumbprint.slice(0, 8)}…` : "(unconfirmed)"}
                          {s.acceptedThumbprints.length > 0 &&
                            `, ${s.acceptedThumbprints.length} older key(s) still accepted`}
                        </span>
                      </span>
                      <Button
                        variant="plain"
                        onClick={() => removeTangServer(i)}
                        isDisabled={isDisabled}
                        aria-label={`Remove Tang server ${s.url}`}
                        style={{ color: "var(--pf-t--global--color--status--danger--100)" }}
                      >
                        <TrashIcon />
                      </Button>
                    </div>
                  ))}
                  <Select
                    id="de-tang-picker"
                    isOpen={tangPickerOpen}
                    onOpenChange={setTangPickerOpen}
                    selected={undefined}
                    onSelect={(_e, value) => {
                      const srv = registry.find((r) => r.id === value);
                      if (srv) addRegistryServer(srv);
                    }}
                    toggle={(ref: React.Ref<MenuToggleElement>) => (
                      <MenuToggle
                        ref={ref}
                        onClick={() => setTangPickerOpen((o) => !o)}
                        isExpanded={tangPickerOpen}
                        isDisabled={isDisabled || availableRegistry.length === 0}
                      >
                        {availableRegistry.length === 0
                          ? "No registered servers left - add them under Settings -> Tang servers"
                          : "Add a server from the registry"}
                      </MenuToggle>
                    )}
                  >
                    <SelectList>
                      {availableRegistry.map((r) => (
                        <SelectOption key={r.id} value={r.id} description={r.url}>
                          {r.name}
                        </SelectOption>
                      ))}
                    </SelectList>
                  </Select>
                  <FormHelperText>
                    <HelperText>
                      <HelperTextItem>
                        The confirmed signing thumbprints are copied into the policy, so agents
                        never trust a Tang server blindly.
                      </HelperTextItem>
                    </HelperText>
                  </FormHelperText>
                </FormGroup>
                {content.tang.servers.length > 1 && (
                  <FormGroup
                    label="Servers required to unlock (threshold)"
                    fieldId="de-tang-threshold"
                  >
                    <NumberInput
                      id="de-tang-threshold"
                      value={content.tang.threshold || 1}
                      min={1}
                      max={content.tang.servers.length}
                      onMinus={() => update((d) => (d.tang.threshold = Math.max(1, (d.tang.threshold || 1) - 1)))}
                      onPlus={() =>
                        update((d) => (d.tang.threshold = Math.min(d.tang.servers.length, (d.tang.threshold || 1) + 1)))
                      }
                      onChange={(e) => {
                        const v = Number((e.target as HTMLInputElement).value);
                        if (Number.isInteger(v)) update((d) => (d.tang.threshold = v));
                      }}
                      inputAriaLabel="Tang threshold"
                      minusBtnAriaLabel="Decrease threshold"
                      plusBtnAriaLabel="Increase threshold"
                      isDisabled={isDisabled}
                    />
                  </FormGroup>
                )}
              </>
            )}
          </CardBody>
        </Card>

        <Card>
          <CardTitle>Recovery key</CardTitle>
          <CardBody>
            <FormGroup fieldId="de-escrow">
              <Switch
                id="de-escrow"
                label="Escrow a recovery key on the Bor server"
                isChecked={content.recovery.escrow}
                onChange={(_e, v) => update((d) => (d.recovery.escrow = v))}
                isDisabled={isDisabled}
              />
              <FormHelperText>
                <HelperText>
                  <HelperTextItem>
                    A random 256-bit key, typed at the boot prompt when TPM and Tang both fail.
                    Stored envelope-encrypted; revealing it needs its own permission plus
                    re-authentication, and every reveal is audited.
                  </HelperTextItem>
                </HelperText>
              </FormHelperText>
            </FormGroup>
            {content.recovery.escrow && (
              <>
                <FormGroup label="Rotate every (days, 0 = never)" fieldId="de-rotation-days">
                  <NumberInput
                    id="de-rotation-days"
                    value={content.recovery.rotationIntervalDays}
                    min={0}
                    max={730}
                    onMinus={() =>
                      update((d) => {
                        const cur = d.recovery.rotationIntervalDays;
                        d.recovery.rotationIntervalDays = cur <= 30 ? 0 : cur - 30;
                      })
                    }
                    onPlus={() =>
                      update((d) => {
                        const cur = d.recovery.rotationIntervalDays;
                        d.recovery.rotationIntervalDays = cur === 0 ? 30 : Math.min(730, cur + 30);
                      })
                    }
                    onChange={(e) => {
                      const v = Number((e.target as HTMLInputElement).value);
                      if (Number.isInteger(v)) update((d) => (d.recovery.rotationIntervalDays = v));
                    }}
                    inputAriaLabel="Rotation interval in days"
                    minusBtnAriaLabel="Decrease rotation interval"
                    plusBtnAriaLabel="Increase rotation interval"
                    isDisabled={isDisabled}
                  />
                  <FormHelperText>
                    <HelperText>
                      <HelperTextItem>Default {DEFAULT_ROTATION_DAYS} days; allowed 30-730.</HelperTextItem>
                    </HelperText>
                  </FormHelperText>
                </FormGroup>
                <FormGroup fieldId="de-rotate-after-reveal">
                  <Switch
                    id="de-rotate-after-reveal"
                    label="Replace the key after every reveal"
                    isChecked={content.recovery.rotateAfterReveal}
                    onChange={(_e, v) => update((d) => (d.recovery.rotateAfterReveal = v))}
                    isDisabled={isDisabled}
                  />
                </FormGroup>
                <FormGroup fieldId="de-server-assisted">
                  <Switch
                    id="de-server-assisted"
                    label="Allow server-assisted rotation"
                    isChecked={content.recovery.serverAssistedRotation}
                    onChange={(_e, v) => update((d) => (d.recovery.serverAssistedRotation = v))}
                    isDisabled={isDisabled}
                  />
                  <FormHelperText>
                    <HelperText>
                      <HelperTextItem>
                        When a rotation is due and the node has no other credential, the server
                        releases the current key to that node only, and only while the rotation
                        task exists. The release is audited and the key is retired in the same
                        run.
                      </HelperTextItem>
                    </HelperText>
                  </FormHelperText>
                </FormGroup>
              </>
            )}
          </CardBody>
        </Card>
      </Gallery>

      {/* ── Advanced ─────────────────────────────────────────────────── */}
      <ExpandableSection
        toggleText="Advanced"
        isExpanded={advancedOpen}
        onToggle={(_e, v) => setAdvancedOpen(v)}
      >
        <FormGroup label="Initramfs handling" fieldId="de-initramfs" role="radiogroup">
          <Radio
            id="de-initramfs-verify"
            name="de-initramfs"
            label="Verify only"
            description="Check that the initramfs can unlock with the configured protectors and report the gap"
            isChecked={content.initramfsMode === "INITRAMFS_MODE_VERIFY_ONLY"}
            onChange={() => update((d) => (d.initramfsMode = "INITRAMFS_MODE_VERIFY_ONLY"))}
            isDisabled={isDisabled}
          />
          <Radio
            id="de-initramfs-manage"
            name="de-initramfs"
            label="Manage (dracut)"
            description="Write a dracut drop-in, add the crypttab options and rebuild the initramfs"
            isChecked={content.initramfsMode === "INITRAMFS_MODE_MANAGE"}
            onChange={() => update((d) => (d.initramfsMode = "INITRAMFS_MODE_MANAGE"))}
            isDisabled={isDisabled}
          />
        </FormGroup>
        {content.initramfsMode === "INITRAMFS_MODE_MANAGE" && (
          <LiveAlert
            id="de-initramfs-manage-warning"
            variant="warning"
            isInline
            message="Manage mode rebuilds the initramfs on every node this policy reaches. Roll it out to a pilot group first."
          />
        )}
        <FormGroup fieldId="de-adopt-existing">
          <Checkbox
            id="de-adopt-existing"
            label="Adopt matching existing enrollments"
            description="TPM2 and Tang keyslots that already match the policy count as managed; non-matching ones are replaced after the new slot verifies"
            isChecked={content.adoptExisting}
            onChange={(_e, v) => update((d) => (d.adoptExisting = v))}
            isDisabled={isDisabled}
          />
        </FormGroup>
        <FormGroup label="Minimum volume key size (bits, 0 = default 512)" fieldId="de-min-bits">
          <NumberInput
            id="de-min-bits"
            value={content.minVolumeKeyBits}
            min={0}
            max={4096}
            onMinus={() => update((d) => (d.minVolumeKeyBits = Math.max(0, d.minVolumeKeyBits - 128)))}
            onPlus={() => update((d) => (d.minVolumeKeyBits = Math.min(4096, d.minVolumeKeyBits + 128)))}
            onChange={(e) => {
              const v = Number((e.target as HTMLInputElement).value);
              if (Number.isInteger(v)) update((d) => (d.minVolumeKeyBits = v));
            }}
            inputAriaLabel="Minimum volume key bits"
            minusBtnAriaLabel="Decrease minimum key bits"
            plusBtnAriaLabel="Increase minimum key bits"
            isDisabled={isDisabled}
          />
          <FormHelperText>
            <HelperText>
              <HelperTextItem>
                512-bit XTS keys are AES-256; the compliance default. Ciphers other than{" "}
                <code>aes-xts-plain64</code> report non-compliant unless listed under allowed
                ciphers in the raw content.
              </HelperTextItem>
            </HelperText>
          </FormHelperText>
        </FormGroup>
      </ExpandableSection>

      {/* ── Resulting protector summary ──────────────────────────────── */}
      <FormGroup label="Resulting keyslot layout" fieldId="de-preview">
        <div id="de-preview">
          {content.tpm2.enabled && <Label color="green">TPM2 - unattended boot on an intact chain</Label>}{" "}
          {content.tang.enabled && (
            <Label color="green">
              Tang ({content.tang.servers.length || "no"} server
              {content.tang.servers.length === 1 ? "" : "s"}) - unattended boot on the corporate network
            </Label>
          )}{" "}
          {content.recovery.escrow && <Label color="green">Recovery key - escrowed, typed when all else fails</Label>}{" "}
          <Label variant="outline">User passphrase - kept</Label>
        </div>
      </FormGroup>

      <LiveAlert id="de-validation" variant="warning" isInline message={validationError} />
    </Form>
  );
};
