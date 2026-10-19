/* Capture the screenshots used by the user documentation (doc/).
 *
 * Usage: BOR_CA_CERT=/path/to/ca.crt \
 *        [BOR_API=https://localhost:8443] [BOR_UI_HOST=bor.example.com] \
 *        [CHROME_BIN=/usr/bin/google-chrome-stable] [SHOTS=name,name] \
 *        node doc-shoot.js [outDir] <admin-user> <admin-password>
 *
 * BOR_CA_CERT is the server's internal CA certificate (ca.crt in the CA
 * autogen directory). API calls verify the server against it; certificate
 * validation stays on for every other connection.
 *
 * outDir defaults to doc/images/screenshots (resolved from this file).
 *
 * Expects an instance seeded with the demo kit (demo/reset/seed.sh +
 * extras.sql) so no screen is empty. Output: <outDir>/<name>.webp at
 * 1280x800 CSS pixels, 2x device scale, light theme. File names are stable,
 * so re-running before a release replaces the images in place.
 *
 * - One editable DRAFT per policy type is needed for the editor shots: an
 *   existing draft of that type is reused, otherwise the first policy of the
 *   type is copied (Flatpak and Disk encryption get built-in examples).
 *   Created drafts are deleted at the end.
 * - /api/v1/agent-packages is answered with a manifest matching the server
 *   version and the enrollment-token POST with a fake token, so the Deploy
 *   agent wizard renders without a real repository and no token is minted.
 * - The browser resolves BOR_UI_HOST to 127.0.0.1 so URLs in the wizard
 *   show a presentable host name.
 */
import fs from "node:fs";
import https from "node:https";
import path from "node:path";
import crypto from "node:crypto";
import puppeteer from "puppeteer-core";

const API = process.env.BOR_API || "https://localhost:8443";
const UI_HOST = process.env.BOR_UI_HOST || "bor.example.com";
const UI = `https://${UI_HOST}:${new URL(API).port || 443}`;
const CHROME = process.env.CHROME_BIN || "/usr/bin/google-chrome-stable";
const DEFAULT_OUT = path.resolve(import.meta.dirname, "../../images/screenshots");
const args = process.argv.slice(2);
const [OUT, USER, PASS] = args.length >= 3 ? args : [DEFAULT_OUT, ...args];
if (!USER || !PASS) {
  console.error("usage: node doc-shoot.js [outDir] <admin-user> <admin-password>");
  process.exit(1);
}
if (!process.env.BOR_CA_CERT) {
  console.error("BOR_CA_CERT must point to the server's CA certificate (ca.crt)");
  process.exit(1);
}
const apiAgent = new https.Agent({ ca: fs.readFileSync(process.env.BOR_CA_CERT) });

/* tiny API client (cookie session + CSRF double-submit) */
const jar = new Map();
function storeCookies(res) {
  for (const c of res.headers["set-cookie"] ?? []) {
    const [pair] = c.split(";");
    const eq = pair.indexOf("=");
    if (eq > 0) jar.set(pair.slice(0, eq).trim(), pair.slice(eq + 1));
  }
}
async function api(method, p, body) {
  const headers = { "Content-Type": "application/json" };
  if (jar.size) headers.Cookie = [...jar].map(([k, v]) => `${k}=${v}`).join("; ");
  if (jar.has("bor_csrf")) headers["X-CSRF-Token"] = jar.get("bor_csrf");
  const { res, text } = await new Promise((resolve, reject) => {
    const req = https.request(`${API}${p}`, { method, headers, agent: apiAgent }, (r) => {
      let data = "";
      r.setEncoding("utf8");
      r.on("data", (chunk) => { data += chunk; });
      r.on("end", () => resolve({ res: r, text: data }));
    });
    req.on("error", reject);
    req.end(body ? JSON.stringify(body) : undefined);
  });
  storeCookies(res);
  if (res.statusCode < 200 || res.statusCode > 299) {
    throw new Error(`${method} ${p} -> ${res.statusCode}: ${text.slice(0, 200)}`);
  }
  return text ? JSON.parse(text) : null;
}

/* fake agent package manifest (same shape as manifest.json) */
function fakeManifest(version) {
  const names = {
    deb: { amd64: `deb/bor-agent_${version}_amd64.deb`, arm64: `deb/bor-agent_${version}_arm64.deb`, ppc64le: `deb/bor-agent_${version}_ppc64el.deb` },
    rpm: { amd64: `rpm/bor-agent-${version}-1.x86_64.rpm`, arm64: `rpm/bor-agent-${version}-1.aarch64.rpm`, ppc64le: `rpm/bor-agent-${version}-1.ppc64le.rpm` },
    apk: { amd64: `apk/bor-agent_${version}_x86_64.apk`, arm64: `apk/bor-agent_${version}_aarch64.apk`, ppc64le: `apk/bor-agent_${version}_ppc64le.apk` },
    arch: { amd64: `arch/bor-agent-${version}-1-x86_64.pkg.tar.zst`, arm64: `arch/bor-agent-${version}-1-aarch64.pkg.tar.zst`, ppc64le: `arch/bor-agent-${version}-1-ppc64le.pkg.tar.zst` },
  };
  const files = [];
  for (const format of Object.keys(names)) {
    for (const arch of ["amd64", "arm64", "ppc64le"]) {
      const p = names[format][arch];
      files.push({ path: p, format, arch, size: 10_500_000,
        sha256: crypto.createHash("sha256").update(`bor-${p}`).digest("hex") });
    }
  }
  return {
    repo_available: true, server_version: version, version_match: true,
    manifest: { version, apk_version: version, generated_at: new Date().toISOString(), signed: true,
      channels: { deb: { repo: true }, rpm: { repo: true }, apk: { repo: false }, arch: { repo: false } }, files },
  };
}

/* example drafts for types the demo kit has no policy for */
async function flatpakExample() {
  // The remote needs its real GPG key when gpgVerify is on; take it from
  // Flathub's published .flatpakrepo file.
  const repo = await (await fetch("https://dl.flathub.org/repo/flathub.flatpakrepo")).text();
  const gpgKey = /^GPGKey=(.+)$/m.exec(repo)?.[1]?.trim();
  if (!gpgKey) throw new Error("could not read GPGKey from flathub.flatpakrepo");
  return {
    name: "Standard Desktop Apps",
    description: "Verified Flathub applications for staff desktops: browser and office suite kept current, graphics tools installed once, Spotify removed.",
    type: "Flatpak",
    content: {
      remotes: [{ name: "flathub", url: "https://dl.flathub.org/repo/", title: "Flathub", gpgVerify: true,
        gpgKeyData: gpgKey, collectionId: "org.flathub.Stable", subset: "verified",
        filterMode: "FLATPAK_FILTER_MODE_NONE", filterRefs: [] }],
      apps: [
        { appId: "org.mozilla.firefox", remote: "flathub", branch: "stable", state: "FLATPAK_APP_STATE_LATEST", scope: "FLATPAK_SCOPE_SYSTEM", displayName: "Firefox" },
        { appId: "org.libreoffice.LibreOffice", remote: "flathub", branch: "stable", state: "FLATPAK_APP_STATE_LATEST", scope: "FLATPAK_SCOPE_SYSTEM", displayName: "LibreOffice" },
        { appId: "org.gimp.GIMP", remote: "flathub", branch: "stable", state: "FLATPAK_APP_STATE_PRESENT", scope: "FLATPAK_SCOPE_SYSTEM", displayName: "GNU Image Manipulation Program" },
        { appId: "com.spotify.Client", remote: "flathub", branch: "stable", state: "FLATPAK_APP_STATE_ABSENT", scope: "FLATPAK_SCOPE_SYSTEM", deleteData: true, displayName: "Spotify" },
      ],
      autoUpdate: true, autoUpdateIntervalHours: 24, uninstallUnused: true,
    },
  };
}
function diskEncryptionExample() {
  return {
    name: "Laptops - Full-Disk Encryption",
    description: "System volumes must be encrypted. TPM 2.0 unlock bound to Secure Boot, recovery keys escrowed and rotated every 180 days.",
    type: "DiskEncryption",
    content: {
      requireEncryption: true,
      volumeScope: "LUKS_VOLUME_SCOPE_SYSTEM",
      tpm2: { enabled: true, pcrProfile: "TPM_PCR_PROFILE_SECURE_BOOT", autoReseal: true },
      recovery: { escrow: true, rotationIntervalDays: 180, rotateAfterReveal: true, serverAssistedRotation: true },
      initramfsMode: "INITRAMFS_MODE_VERIFY_ONLY",
    },
  };
}

/* Policy type -> output name of its editor screenshot. */
const EDITOR_SHOTS = {
  Firefox: "policy-editor-firefox",
  Chrome: "policy-editor-chrome",
  Edge: "policy-editor-edge",
  Thunderbird: "policy-editor-thunderbird",
  Kconfig: "policy-editor-kconfig",
  Dconf: "policy-editor-dconf",
  Polkit: "policy-editor-polkit",
  Firewalld: "policy-editor-firewalld",
  Package: "policy-editor-packages",
  SessionAccess: "policy-editor-session-access",
  Flatpak: "policy-editor-flatpak",
  DiskEncryption: "policy-editor-disk-encryption",
  Systemd: "policy-editor-systemd",
};

const ONLY = process.env.SHOTS ? new Set(process.env.SHOTS.split(",")) : null;
const want = (...names) => !ONLY || names.some((n) => ONLY.has(n));

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
async function shot(page, name, selector) {
  // Park the pointer in a corner so no hover highlight ends up in the image.
  await page.mouse.move(1279, 799);
  await sleep(800);
  const file = path.join(OUT, `${name}.webp`);
  const opts = { path: file, type: "webp", quality: 85 };
  if (selector) {
    const el = await page.waitForSelector(selector, { timeout: 10000 });
    await clipShot(page, el, opts);
  } else {
    await page.screenshot(opts);
  }
  console.log("saved", name);
}
/* Element screenshot with a 16px margin, so dialogs keep their rounded
 * corners on the backdrop instead of showing clipped corner artifacts. */
async function clipShot(page, el, opts) {
  const b = await el.boundingBox();
  const m = 16;
  const x = Math.max(0, b.x - m), y = Math.max(0, b.y - m);
  await page.screenshot({ ...opts, clip: { x, y, width: Math.min(1280 - x, b.width + 2 * m), height: Math.min(800 - y, b.height + 2 * m) } });
}
async function clickByText(page, selector, text) {
  const ok = await page.evaluate((sel, t) => {
    const el = [...document.querySelectorAll(sel)].find((e) => e.textContent.trim().includes(t));
    if (el) { el.scrollIntoView({ block: "center" }); el.click(); return true; }
    return false;
  }, selector, text);
  if (!ok) throw new Error(`no ${selector} containing "${text}"`);
}
async function go(page, route, waitFor = "h1") {
  await page.goto(`${UI}${route}`, { waitUntil: "networkidle2", timeout: 30000 });
  await page.waitForSelector(waitFor, { timeout: 20000 });
  await sleep(500);
}

(async () => {
  fs.mkdirSync(OUT, { recursive: true });
  await api("POST", "/api/v1/auth/login", { username: USER, password: PASS });
  const { version } = await api("GET", "/api/v1/version");
  console.log(`api: logged in, server ${version}`);

  /* One editable draft per type. */
  const all = await api("GET", "/api/v1/policies/all");
  const created = [];
  const draftFor = {};
  for (const type of Object.keys(EDITOR_SHOTS)) {
    if (ONLY && !ONLY.has(EDITOR_SHOTS[type])) continue;
    // Copy a released policy (the demo's realistic baselines) into a new
    // draft so the editor renders editable; built-in examples otherwise.
    const ofType = all.filter((p) => p.type === type);
    let src = ofType.find((p) => p.state === "released") || ofType[0];
    if (!src && type === "Flatpak") src = await flatpakExample();
    if (!src && type === "DiskEncryption") src = diskEncryptionExample();
    if (!src) { console.log("no example for", type); continue; }
    const content = typeof src.content === "string" ? src.content : JSON.stringify(src.content, null, 2);
    const p = await api("POST", "/api/v1/policies/all", { name: src.name, description: src.description, type, content });
    created.push(p.id);
    draftFor[type] = p.id;
  }
  const cleanup = async () => {
    for (const id of created.splice(0)) {
      try { await api("DELETE", `/api/v1/policies/all/${id}`); } catch { /* already gone */ }
    }
  };

  const browser = await puppeteer.launch({
    executablePath: CHROME,
    headless: true,
    args: ["--no-sandbox", "--ignore-certificate-errors", "--window-size=1280,800",
      `--host-resolver-rules=MAP ${UI_HOST} 127.0.0.1`],
    defaultViewport: { width: 1280, height: 800, deviceScaleFactor: 2 },
  });
  try {
    const page = await browser.newPage();
    page.on("dialog", (d) => d.accept().catch(() => {}));
    await page.emulateMediaFeatures([{ name: "prefers-color-scheme", value: "light" }]);
    await page.setRequestInterception(true);
    page.on("request", (req) => {
      const u = new URL(req.url());
      if (u.pathname === "/api/v1/agent-packages") {
        return req.respond({ status: 200, contentType: "application/json", body: JSON.stringify(fakeManifest(version)) });
      }
      if (req.method() === "POST" && /^\/api\/v1\/node-groups\/[^/]+\/tokens$/.test(u.pathname)) {
        return req.respond({ status: 200, contentType: "application/json", body: JSON.stringify({
          token: crypto.createHash("sha256").update("demo-enrollment-token").digest("hex").slice(0, 32),
          node_group_id: u.pathname.split("/")[4], expires_at: new Date(Date.now() + 5 * 60000).toISOString() }) });
      }
      req.continue();
    });

    /* Login (two-step). The username step is the login screenshot. */
    await page.goto(`${UI}/`, { waitUntil: "networkidle2", timeout: 30000 });
    await page.waitForSelector("#login-username", { timeout: 15000 });
    await page.type("#login-username", USER);
    if (want("login")) await shot(page, "login");
    await page.click('button[type="submit"]');
    await page.waitForSelector("#login-password", { timeout: 15000 });
    await page.type("#login-password", PASS);
    await Promise.all([
      page.waitForNavigation({ waitUntil: "networkidle2", timeout: 30000 }).catch(() => {}),
      page.click('button[type="submit"]'),
    ]);
    await page.waitForSelector("h1", { timeout: 20000 });
    console.log("ui: logged in");

    if (want("dashboard")) { await go(page, "/"); await sleep(1000); await shot(page, "dashboard"); }

    if (want("nodes", "node-details")) {
      await go(page, "/nodes", "table tbody tr");
      if (want("nodes")) await shot(page, "nodes");
      if (want("node-details")) {
        await page.click("table tbody tr");
        await sleep(800);
        await shot(page, "node-details");
      }
    }

    if (want("node-groups")) { await go(page, "/node-groups", "table"); await shot(page, "node-groups"); }

    if (want("deploy-agent")) {
      await go(page, "/nodes");
      await page.click('button[aria-label="Deploy agent"]');
      await page.waitForSelector('[role="dialog"]', { timeout: 10000 });
      await sleep(500);
      try {
        const toggle = await page.waitForSelector('[role="dialog"] [aria-label="Node group for enrollment"]', { timeout: 5000 });
        await toggle.click();
        await sleep(300);
        await clickByText(page, '[role="option"], [role="menuitem"], li button', "Finance Workstations");
        await sleep(300);
        await clickByText(page, '[role="dialog"] button', "Add enrollment step");
        await page.waitForFunction(() => document.querySelector('[role="dialog"]')?.textContent.includes("--token-file"), { timeout: 5000 });
      } catch (e) {
        console.log("deploy wizard: enrollment step skipped:", e.message);
      }
      await shot(page, "deploy-agent", '[role="dialog"]');
      await page.keyboard.press("Escape");
    }

    if (want("policies")) { await go(page, "/policies", "table"); await shot(page, "policies"); }

    if (want("policy-create-wizard")) {
      await go(page, "/policies/new", '[role="group"][aria-label="Policy types"]');
      await page.evaluate(() => {
        const card = [...document.querySelectorAll(".pf-v6-c-card")].find((c) =>
          [...c.querySelectorAll(".pf-v6-c-card__title")].some((t) => t.textContent.trim() === "Firefox"));
        (card?.querySelector("input") || card)?.click();
      });
      await shot(page, "policy-create-wizard");
    }

    for (const [type, name] of Object.entries(EDITOR_SHOTS)) {
      if (!want(name) || !draftFor[type]) continue;
      await go(page, `/policies/${draftFor[type]}/edit`, 'button[role="tab"]');
      await clickByText(page, 'button[role="tab"]', "Configuration");
      await sleep(1000);
      await shot(page, name);
    }

    if (want("policy-bindings")) { await go(page, "/policy-bindings", "table"); await shot(page, "policy-bindings"); }
    if (want("compliance")) { await go(page, "/compliance"); await sleep(800); await shot(page, "compliance"); }

    if (want("disk-encryption", "disk-encryption-reveal")) {
      await go(page, "/disk-encryption", 'table[aria-label="LUKS volumes"]');
      if (want("disk-encryption")) await shot(page, "disk-encryption");
      if (want("disk-encryption-reveal")) {
        // Fill in the step-up form but do not submit: the screenshot shows
        // what an operator is asked for, no key is revealed.
        await page.click('button[aria-label="Actions for volume luks-root on fin-ws-01"]');
        await sleep(300);
        await clickByText(page, '[role="menuitem"] button, [role="menuitem"], li button', "Show recovery key");
        await page.waitForSelector("#reveal-reason", { timeout: 10000 });
        await page.type("#reveal-reason", "INC-4102: user locked out after a firmware update");
        await page.type("#reveal-password", "decoy-password");
        await shot(page, "disk-encryption-reveal", '[role="dialog"]');
        await page.keyboard.press("Escape");
      }
    }

    if (want("audit-logs")) { await go(page, "/audit-logs", "table"); await shot(page, "audit-logs"); }

    for (const [tab, name] of [["users", "settings-users"], ["roles", "settings-roles"], ["user-groups", "settings-user-groups"],
      ["agent-notifications", "settings-agent-notifications"], ["mfa-settings", "settings-mfa-policy"],
      ["flatpak-repos", "settings-flatpak-repositories"]]) {
      if (!want(name)) continue;
      await go(page, `/settings?tab=${tab}`);
      await sleep(600);
      await shot(page, name);
    }

    if (want("account-security", "mfa-totp-setup")) {
      await go(page, "/");
      // The masthead user menu shows the display name, not the username.
      await page.evaluate(() => [...document.querySelectorAll(".pf-v6-c-masthead .pf-v6-c-menu-toggle")].pop()?.click());
      await sleep(300);
      await clickByText(page, '[role="menuitem"], li button', "Account security");
      await page.waitForSelector('[role="dialog"]', { timeout: 10000 });
      await sleep(600);
      if (want("account-security")) await shot(page, "account-security", '[role="dialog"]');
      if (want("mfa-totp-setup")) {
        // Opens the TOTP setup with a freshly generated secret for the
        // throwaway screenshot instance; it is never confirmed.
        await clickByText(page, '[role="dialog"] button', "Set up");
        await page.waitForFunction(() => document.querySelectorAll('[role="dialog"]').length > 1
          || document.querySelector('[role="dialog"] img, [role="dialog"] canvas, [role="dialog"] svg[role="img"]'), { timeout: 10000 });
        await sleep(800);
        const dialogs = await page.$$('[role="dialog"]');
        await page.mouse.move(1279, 799);
        await clipShot(page, dialogs[dialogs.length - 1], { path: path.join(OUT, "mfa-totp-setup.webp"), type: "webp", quality: 85 });
        console.log("saved mfa-totp-setup");
      }
    }
  } finally {
    await browser.close();
    await cleanup();
  }
  console.log("done");
})().catch((e) => {
  console.error("FAILED:", e.message);
  process.exit(1);
});
