import { useEffect, useState } from "preact/hooks";
import type { Exposure as ExposureInfo, ExposureMode } from "./api";
import { api, errText, inWails, openExternal } from "./api";
import { ask } from "./Confirm";
import { t } from "./i18n";

// WHERE THE GATEWAY CAN BE REACHED FROM.
//
// Three plain choices: local (127.0.0.1, this machine alone), LAN (this machine's address in the
// local network) and WAN (0.0.0.0, every interface). A change is kept and the gateway restarts to
// listen there; choosing WAN asks for an explicit yes, because the token travels over plain http
// unless https is in front.

function isLoopbackPage(): boolean {
  return ["localhost", "127.0.0.1", "[::1]"].includes(location.hostname);
}

export default function Exposure() {
  const [cur, setCur] = useState<ExposureInfo | null>(null);
  const [pick, setPick] = useState<ExposureMode>("local");
  const [risk, setRisk] = useState(false);
  const [busy, setBusy] = useState(false);
  const [restarting, setRestarting] = useState(false);
  const [err, setErr] = useState("");
  useEffect(() => {
    api
      .exposure()
      .then((e) => {
        setCur(e);
        setPick(e.mode);
      })
      // A gateway that cannot change it (an older one) shows nothing.
      .catch(() => setCur(null));
  }, []);
  if (!cur) {
    return null;
  }
  const port = cur.listen.slice(cur.listen.lastIndexOf(":") + 1);
  const options: { mode: ExposureMode; title: string; addr: string; hint: string }[] = [
    {
      mode: "local",
      title: t("Local"),
      addr: `127.0.0.1:${port}`,
      hint: t("Only this computer. The safest choice."),
    },
    {
      mode: "lan",
      title: t("LAN"),
      addr: `${cur.lan_ip || t("your network address")}:${port}`,
      hint: t("Other devices in your home or office network."),
    },
    {
      mode: "wan",
      title: t("WAN"),
      addr: `0.0.0.0:${port}`,
      hint: t("Every network interface: the internet too if this computer is public."),
    },
  ];
  const changed = pick !== cur.mode;
  const apply = async () => {
    // Moving to local from another device cuts this very page off.
    if (pick === "local" && !isLoopbackPage()) {
      const yes = await ask(
        t(
          "You are using Kitties from another device: with Local you will not be able to reach it from here. Continue?",
        ),
        t("Continue"),
      );
      if (!yes) {
        return;
      }
    }
    setBusy(true);
    setErr("");
    try {
      await api.setExposure(pick, risk);
      setRestarting(true);
      // The gateway restarts a moment after answering: wait for it, then load it again.
      let tries = 0;
      const id = setInterval(async () => {
        tries++;
        try {
          const e = await api.exposure();
          if (e.mode === pick) {
            clearInterval(id);
            location.reload();
          }
        } catch {
          /* it is restarting */
        }
        if (tries > 30) {
          clearInterval(id);
          setRestarting(false);
          setErr(
            t(
              "Kitties did not answer here after the change: it may now listen only elsewhere. Try {addr}.",
              { addr: options.find((o) => o.mode === pick)?.addr || "" },
            ),
          );
        }
      }, 1000);
    } catch (e) {
      setErr(errText(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <div class="mt-6 pt-4 border-t border-white/5" data-testid="exposure">
      <div class="section-title !mx-0">{t("Network access")}</div>
      <p class="text-xs text-[var(--t3)] mb-2">
        {t("Where Kitties can be reached from. Now: {addr}.", { addr: cur.listen })}
      </p>
      <div class="flex flex-col gap-2" role="radiogroup">
        {options.map((o, i) => (
          <label
            key={o.mode}
            class="callout flex items-start gap-2 cursor-pointer"
            data-testid={`exposure-${o.mode}`}
          >
            <input
              type="radio"
              name="exposure"
              class="mt-1"
              checked={pick === o.mode}
              disabled={!cur.can_change || busy || restarting}
              onChange={() => {
                setPick(o.mode);
                setRisk(false);
              }}
            />
            <span>
              <span class="font-semibold text-sm">
                {i + 1}) {o.title}
              </span>
              <span class="text-xs text-[var(--t3)]"> · {o.addr}</span>
              <span class="block text-xs text-[var(--t3)]">{o.hint}</span>
            </span>
          </label>
        ))}
      </div>
      {pick === "wan" && changed && (
        <div class="callout mt-2" role="alert" data-testid="exposure-warning">
          <div class="font-semibold text-sm text-[var(--c-amber)]">
            {t("WAN listens on every interface")}
          </div>
          <p class="text-xs text-[var(--t3)] mt-1">
            {t(
              "The page and its token travel over plain http and the token is the only password. Use it only behind a firewall or a VPN, or put https in front.",
            )}
          </p>
          <label class="flex items-center gap-2 mt-2 text-sm cursor-pointer">
            <input
              type="checkbox"
              checked={risk}
              onChange={() => setRisk(!risk)}
              data-testid="exposure-accept"
            />
            {t("I understand the risk")}
          </label>
        </div>
      )}
      {!cur.can_change && cur.why && (
        <p class="text-xs text-[var(--t3)] mt-2" data-testid="exposure-locked">
          {cur.why}
        </p>
      )}
      {restarting && (
        <p class="text-sm mt-2" role="status" data-testid="exposure-restarting">
          <span class="spinner" /> {t("Saved: restarting Kitties…")}
        </p>
      )}
      {cur.can_change && (
        <button
          class="btn-accent mt-3 px-3 py-1.5 text-sm"
          disabled={!changed || busy || restarting || (pick === "wan" && !risk)}
          onClick={apply}
          data-testid="exposure-apply"
        >
          {t("Apply and restart")}
        </button>
      )}
      {err && (
        <p class="text-sm text-[var(--c-red)] mt-2" role="alert">
          {err}
        </p>
      )}
      {inWails() && (
        <InBrowser
          addr={cur.mode === "lan" && cur.lan_ip ? `${cur.lan_ip}:${port}` : `127.0.0.1:${port}`}
          phone={cur.mode !== "local" && cur.lan_ip ? `${cur.lan_ip}:${port}` : ""}
        />
      )}
      <RotateToken info={cur} />
    </div>
  );
}

// RotateToken replaces the gateway's token, for when it got out: every browser and program that
// used the old one is signed out. The new one is shown only where the old one was: the console
// that started the gateway, its token file and this app on that machine.
function RotateToken({ info }: { info: ExposureInfo }) {
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  if (!info.token_rotate) {
    return info.token_why ? (
      <p class="text-xs text-[var(--t4)] mt-3" data-testid="token-fixed">
        {info.token_why}
      </p>
    ) : null;
  }
  const rotate = async () => {
    const yes = await ask(
      inWails()
        ? t(
            "Make a new token? Every browser and program that uses the old one is signed out; this app takes the new one by itself.",
          )
        : t(
            "Make a new token? Every browser and program that uses the old one is signed out, this page too. The new one is shown in the console where Kitties runs (or with kitties token) and in the desktop app.",
          ),
      t("Make a new token"),
    );
    if (!yes) {
      return;
    }
    setBusy(true);
    setErr("");
    try {
      await api.rotateToken();
      // The gateway restarts with it: load again, which signs this page in again or asks.
      setTimeout(() => location.reload(), 2500);
    } catch (e) {
      setErr(errText(e));
      setBusy(false);
    }
  };
  return (
    <div class="mt-4" data-testid="rotate-token">
      <button class="btn-ghost px-3 py-1.5 text-sm" disabled={busy} onClick={rotate}>
        {busy ? t("Restarting Kitties…") : t("Make a new token")}
      </button>
      <p class="text-xs text-[var(--t4)] mt-1">
        {t("If the token got out, a new one signs everybody out.")}
      </p>
      {err && (
        <p class="text-sm text-[var(--c-red)] mt-2" role="alert">
          {err}
        </p>
      )}
    </div>
  );
}

// InBrowser is how the person who runs the app uses Kitties from a browser too: the same page,
// at the gateway's address, signed in with its token. Only the app shows the token (it runs on
// the machine the gateway runs on); a browser never gets it from the gateway.
function InBrowser({ addr, phone }: { addr: string; phone: string }) {
  const [token, setToken] = useState("");
  const [shown, setShown] = useState(false);
  const [copied, setCopied] = useState(false);
  const [qr, setQr] = useState("");
  useEffect(() => {
    window.go?.main?.App?.Gateway()
      .then((c) => setToken(c.token))
      .catch(() => setToken(""));
  }, []);
  useEffect(() => {
    if (!phone || !token) {
      setQr("");
      return;
    }
    window.go?.main?.App?.SignInQR(`http://${phone}`)
      .then(setQr)
      .catch(() => setQr(""));
  }, [phone, token]);
  const url = `http://${addr}`;
  // Opened from here it signs in by itself: the token rides after the #, which no server sees.
  const signedIn = token ? `${url}/#signin=${encodeURIComponent(token)}` : url;
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(token);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      setShown(true);
    }
  };
  return (
    <div class="callout mt-4" data-testid="in-browser">
      <div class="font-semibold text-sm">{t("Use it from a browser")}</div>
      <p class="text-xs text-[var(--t3)] mt-1">
        {t("The same page as this app. Open it and sign in with the token.")}
      </p>
      <div class="flex flex-wrap items-center gap-2 mt-2">
        <code class="font-mono text-xs">{url}</code>
        <button
          class="btn-ghost px-2 py-1 text-xs"
          onClick={() => openExternal(signedIn)}
          data-testid="in-browser-open"
        >
          {t("Open in the browser")}
        </button>
      </div>
      {token && (
        <div class="flex flex-wrap items-center gap-2 mt-2">
          <span class="text-xs text-[var(--t3)]">{t("Token")}:</span>
          <code class="font-mono text-xs" data-testid="in-browser-token">
            {shown ? token : "••••••••••••••••"}
          </code>
          <button class="btn-ghost px-2 py-1 text-xs" onClick={() => setShown(!shown)}>
            {shown ? t("Hide") : t("Show")}
          </button>
          <button class="btn-ghost px-2 py-1 text-xs" onClick={copy} data-testid="in-browser-copy">
            {copied ? t("Copied") : t("Copy")}
          </button>
        </div>
      )}
      {qr ? (
        <div class="flex items-center gap-3 mt-3" data-testid="in-browser-qr">
          <img
            src={qr}
            alt={t("QR code to open Kitties on a phone")}
            class="w-32 h-32 rounded bg-white p-1"
          />
          <p class="text-xs text-[var(--t3)]">
            {t(
              "On a phone in the same network, point the camera here: Kitties opens signed in. Anyone who sees this code can get in.",
            )}
          </p>
        </div>
      ) : (
        <p class="text-xs text-[var(--t4)] mt-2">
          {t("To open it from a phone with a QR code, choose LAN above.")}
        </p>
      )}
      <p class="text-xs text-[var(--t4)] mt-2">
        {t("Keep the token to yourself: it is the only password of the page.")}
      </p>
    </div>
  );
}
