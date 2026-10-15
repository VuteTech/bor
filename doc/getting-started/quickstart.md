# Quick start

This page is the shortest path from nothing to a working Bor setup: one
server, one enrolled desktop, and one Firefox policy that you can see take
effect on the endpoint. Each step links to the detailed page if you need more
options. Plan for about 20 minutes.

The example uses a Debian or Ubuntu server and the hostname
`bor.example.com`. For other distributions, follow the matching tab in
[Installing the server](../installation/server.md).

## Before you begin

You need:

- A Linux host for the server that desktops can reach by name on TCP ports
  8443 and 8444. See [Requirements](../installation/requirements.md).
- One Linux desktop with Firefox installed, to act as the first managed node.
- Root access (`sudo`) on both machines.

!!! note "Default port in this guide"
    This guide keeps the default web port 8443, which is fine for a test
    setup. For production, use port 443 instead; see
    [Choose the web port](../installation/server.md#step-6-choose-the-web-port).
    Other pages of this documentation assume port 443 and write URLs
    without a port.

## Step 1: Install the server

1. Install PostgreSQL first, so the package can create the database for you:

    ```bash
    sudo apt install postgresql
    ```

2. Download the `bor-server` package for your architecture from the
   [GitHub releases](https://github.com/VuteTech/Bor/releases) page or
   [getbor.dev/download](https://getbor.dev/download/) and install it:

    ```bash
    sudo apt install ./bor-server_*_amd64.deb
    ```

3. Copy the initial admin password from the installer output. It looks like
   this:

    ```text
    bor-server: ======================================================
    bor-server:  Initial admin credentials
    bor-server:    Username: admin
    bor-server:    Password: 3f9c...
    bor-server:  Change this password immediately after first login.
    bor-server: ======================================================
    ```

    If you missed it, it is stored as `admin_password` in
    `/etc/bor/server.yaml`.

4. Add the server's DNS name to the certificate. Edit `/etc/bor/server.yaml`
   and set `server.hostnames`:

    ```yaml
    server:
      hostnames:
        - "bor.example.com"
    ```

5. Open the firewall (if one is active) and start the service:

    ```bash
    sudo ufw allow 8443/tcp
    sudo ufw allow 8444/tcp
    sudo systemctl enable --now bor-server
    ```

6. Check that it started:

    ```bash
    sudo journalctl -u bor-server -n 50
    ```

    Look for `HTTPS + enrollment gRPC listening on :8443` and
    `Agent policy gRPC (mTLS) listening on :8444`.

Details: [Installing the server](../installation/server.md).

## Step 2: Log in and secure the admin account

1. Open `https://bor.example.com:8443` in a browser. The certificate is
   issued by Bor's own internal CA, so the browser warns about it the first
   time; accept the warning or trust the CA as described in
   [Installing the server](../installation/server.md#trust-the-internal-ca-in-your-browser).
2. Enter `admin` as **Username**, click **Continue**, enter the password and
   click **Log in**.
3. Open the user menu in the top-right corner and choose
   **Account security**. On the **Change Password** tab, set a new password
   (at least 12 characters).
4. Remove the `admin_password` line from `/etc/bor/server.yaml`. It is only
   used to create the first account and should not stay on disk.

## Step 3: Create a node group

1. In the sidebar, go to **Fleet -> Node Groups**.
2. Click **Create Node Group**, enter a name such as `Pilot desktops`, and
   click **Create**.

Details: [Node groups](../user-guide/node-groups.md).

## Step 4: Deploy the agent on the desktop

![Deploy agent dialog showing the generated install and enrollment script for a node group](../images/screenshots/deploy-agent.webp){ loading=lazy }

1. In the web UI, click the **Deploy agent** button on the **Nodes** page
   (or the download icon in the top bar).
2. Pick the desktop's platform (for example **Debian / Ubuntu**) and
   architecture.
3. In **Enroll into node group...**, select `Pilot desktops` and click
   **Add enrollment step**. The script now contains a one-time token that is
   valid for 5 minutes.
4. Click the copy icon and paste the script into a root-capable shell on the
   desktop. It:
    1. trusts the server's internal CA,
    2. adds the package repository hosted by your server and installs
       `bor-agent`,
    3. points `/etc/bor/config.yaml` at your server,
    4. enrolls the agent with the token, and
    5. runs `systemctl enable --now bor-agent`.
5. Back in the web UI, open **Fleet -> Nodes**. The desktop appears with its
   hostname and an online status within a few seconds.

Details: [Installing the agent](../installation/agent.md).

## Step 5: Create and release a Firefox policy

1. Go to **Policy -> Policies** and click **Create a policy**.
2. **Type**: choose **Firefox** and click **Next**.
3. **Details**: enter a **Name**, for example `Firefox baseline`, and click
   **Next**.
4. **Configuration**: in the tree on the left, open the
   **Privacy & Telemetry** group and select **Disable Telemetry**. Turn the
   **Value** switch on. Click **Next**.
5. **Review**: check the summary and click **Create policy**.
6. On the confirmation screen, click **Release now** and confirm with
   **Release**. The policy is now released but not yet delivered anywhere.

Details: [Policies](../user-guide/policies.md) and
[Firefox](../policy-types/firefox.md).

## Step 6: Bind the policy to the node group

1. On the same confirmation screen, click **Assign to a node group** (or go
   to **Policy -> Policy Bindings** and click **Create Binding**).
2. Select the policy and the `Pilot desktops` group, leave **Priority** at
   0, and click **Create**.
3. New bindings are created disabled. In the bindings table, turn on the
   switch in the **Binding State** column and confirm with **Enable**.

The server pushes the policy to every node in the group immediately.

Details: [Policy bindings](../user-guide/bindings.md).

## Step 7: Verify on the endpoint

On the desktop:

1. Look at the managed policy file:

    ```bash
    sudo cat /etc/firefox/policies/policies.json
    ```

    It contains `"DisableTelemetry": true` under `"policies"`.

2. Check the agent log:

    ```bash
    sudo journalctl -u bor-agent -n 30
    ```

    You should see a line like
    `Firefox policies synced to /etc/firefox/policies/policies.json (1 policies)`.

3. Restart Firefox and open `about:policies`. The policy is listed as active.

4. Optional: test tamper protection. Edit or delete
   `/etc/firefox/policies/policies.json` as root. The agent restores it
   within moments and logs `Tamper protection: restoring ...`; the event
   also appears in **System -> Audit Logs**.

In the web UI, **Fleet -> Compliance** now shows the policy as
**Compliant** on the node.

## Next steps

- Turn on multi-factor authentication for your account:
  [Multi-factor authentication](../configuration/mfa.md).
- Replace the internal certificate with your own or an ACME certificate:
  [TLS and certificates](../configuration/tls.md).
- Connect LDAP or Active Directory: [LDAP and Active Directory](../configuration/ldap.md).
- Read [Concepts](concepts.md) to understand priorities, merging and the
  policy lifecycle.
