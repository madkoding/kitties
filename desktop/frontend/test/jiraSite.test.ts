import assert from "node:assert/strict";
import { test } from "node:test";
import { jiraSite } from "../src/jiraSite.ts";

test("jiraSite reads a Jira Site like the gateway does", () => {
  assert.equal(jiraSite(""), null);
  assert.deepEqual(jiraSite("https://acme.atlassian.net"), {
    site: "https://acme.atlassian.net",
    changed: false,
    cloud: true,
  });
  assert.deepEqual(jiraSite("acme.atlassian.net/jira/your-work"), {
    site: "https://acme.atlassian.net",
    changed: true,
    cloud: true,
  });
  assert.equal(jiraSite("http://Acme.atlassian.net/")?.site, "https://acme.atlassian.net");
  assert.deepEqual(jiraSite("https://jira.corp.example/jira/browse/K-1"), {
    site: "https://jira.corp.example/jira",
    changed: true,
    cloud: false,
  });
  assert.equal(
    jiraSite("jira.corp.example:8443/secure/Dashboard.jspa")?.site,
    "https://jira.corp.example:8443",
  );
  assert.equal(jiraSite("not an address")?.site, "");
  assert.equal(jiraSite("ftp://acme.atlassian.net")?.site, "");
});
