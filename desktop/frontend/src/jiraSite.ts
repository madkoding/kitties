// jiraSite mirrors the gateway's reading of a Jira Site (internal/connector/jira.go): https
// when no scheme was typed, without the page it was copied from, and only the host on Jira
// Cloud. It says what will be used, so the form can tell the person while they type.
export interface SiteCheck {
  // site is the address the calls will go to; empty when the text is not an address.
  site: string;
  // changed says the site differs from what was typed.
  changed: boolean;
  cloud: boolean;
}

const PAGES = [
  "/rest/",
  "/browse/",
  "/secure/",
  "/projects/",
  "/plugins/",
  "/issues/",
  "/servicedesk/",
  "/login.jsp",
];

export function jiraSite(raw: string): SiteCheck | null {
  const typed = raw.trim();
  if (!typed) {
    return null;
  }
  const withScheme = typed.includes("://") ? typed : "https://" + typed;
  let u: URL;
  try {
    u = new URL(withScheme);
  } catch {
    return { site: "", changed: true, cloud: false };
  }
  if (
    (u.protocol !== "https:" && u.protocol !== "http:") ||
    (!u.hostname.includes(".") && u.hostname !== "localhost")
  ) {
    return { site: "", changed: true, cloud: false };
  }
  const host = u.host.toLowerCase();
  const cloud = host.endsWith(".atlassian.net") || host.endsWith(".jira.com");
  let site: string;
  if (cloud) {
    site = "https://" + host;
  } else {
    let path = u.pathname;
    for (const page of PAGES) {
      const i = (path + "/").indexOf(page);
      if (i >= 0) {
        path = path.slice(0, i);
      }
    }
    site = u.protocol + "//" + u.host + path.replace(/\/+$/, "");
  }
  return { site, changed: site !== typed.replace(/\/+$/, ""), cloud };
}
