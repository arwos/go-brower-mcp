// src/background/page-extractor.ts
async function pageExtractor(options) {
  const clean = (value) => value.replace(/[ \t\f\v]+/g, " ").replace(/\n{3,}/g, "\n\n").trim();
  const escape = (value) => value.replace(/[\\`*_\[\]<>]/g, "\\$&");
  const cleanLinkText = (value) => clean(value).replace(/\s*\n\s*/g, " ").replace(/\s+/g, " ").trim();
  const markdownLink = (label, href) => {
    const url = new URL(href, location.href);
    if (url.protocol !== "http:" && url.protocol !== "https:") return cleanLinkText(label);
    const safeLabel = cleanLinkText(label) || url.href;
    return `[${escape(safeLabel)}](${url.href.replace(/[()\\]/g, "\\$&")})`;
  };
  const maxText = Math.min(Number(options["maxTextChars"] ?? 5e4), 2e5);
  const maxLinks = Math.min(Number(options["maxLinks"] ?? options["limit"] ?? 100), 500);
  const maxImages = Math.min(Number(options["maxImages"] ?? 5), 10);
  const maxImageBytes = 15e5;
  const root = document.body.cloneNode(true);
  root.querySelectorAll('script,style,noscript,template,svg,[hidden],[aria-hidden="true"]').forEach((x) => x.remove());
  const inline = (node) => {
    if (node.nodeType === Node.TEXT_NODE) return escape(node.textContent ?? "");
    if (node.nodeType !== Node.ELEMENT_NODE) return "";
    const element = node;
    const value = Array.from(element.childNodes).map(inline).join("");
    if (element.tagName === "A") {
      const href = element.href;
      return /^https?:/i.test(href) ? markdownLink(element.textContent ?? value, href) : value;
    }
    if (element.tagName === "STRONG" || element.tagName === "B") return value ? `**${value}**` : "";
    if (element.tagName === "EM" || element.tagName === "I") return value ? `*${value}*` : "";
    if (element.tagName === "CODE") return value ? `\`${value}\`` : "";
    if (element.tagName === "BR") return "\n";
    return value;
  };
  const block = (node, depth = 0) => {
    if (node.nodeType === Node.TEXT_NODE) return escape(node.textContent ?? "");
    if (node.nodeType !== Node.ELEMENT_NODE) return "";
    const element = node;
    if (element.tagName === "TABLE") {
      const rows = Array.from(element.rows).map((row) => Array.from(row.cells).map((cell) => clean(inline(cell)).replace(/\|/g, "\\|"))).filter((row) => row.some(Boolean));
      if (!rows.length) return "";
      const width = Math.max(...rows.map((row) => row.length));
      const normalized = rows.map((row) => [...row, ...Array(width - row.length).fill("")]);
      return [`| ${normalized[0].join(" | ")} |`, `| ${normalized[0].map(() => "---").join(" | ")} |`, ...normalized.slice(1).map((row) => `| ${row.join(" | ")} |`)].join("\n") + "\n\n";
    }
    const content = Array.from(element.childNodes).map((child) => block(child, depth + 1)).join("");
    const value = inline(element);
    if (/^H[1-6]$/.test(element.tagName)) return `${"#".repeat(Number(element.tagName[1]))} ${clean(value)}

`;
    if (element.tagName === "LI") return `${"  ".repeat(Math.max(0, depth - 2))}- ${clean(value)}
`;
    if (element.tagName === "PRE") return `

\`\`\`
${element.textContent?.trim() ?? ""}
\`\`\`

`;
    if (element.tagName === "HR") return "\n\n---\n\n";
    if (["P", "BLOCKQUOTE", "ARTICLE", "SECTION", "MAIN", "HEADER", "FOOTER", "FIGURE", "FIGCAPTION", "DIV", "ASIDE", "NAV", "FORM", "UL", "OL", "DL", "DD", "DT", "ADDRESS"].includes(element.tagName)) return `${clean(content || value)}

`;
    return value;
  };
  let text = clean(block(root));
  const truncated = text.length > maxText;
  text = text.slice(0, maxText);
  const seen = /* @__PURE__ */ new Set();
  const links = Array.from(document.querySelectorAll("a[href]")).map((a) => ({ text: clean(a.innerText) || clean(a.getAttribute("aria-label") || "") || a.href, url: a.href })).filter((l) => /^https?:/i.test(l.url) && !seen.has(l.url) && (seen.add(l.url), true)).filter((l) => !options["domain"] || new URL(l.url).hostname === options["domain"]).slice(0, maxLinks);
  const warnings = [];
  const images = [];
  if (options["includeImages"]) {
    const srcs = [...new Set(Array.from(document.images).map((i) => i.currentSrc || i.src).filter((src) => /^https?:|^data:image\//i.test(src)))].slice(0, maxImages);
    for (const src of srcs) {
      try {
        if (src.startsWith("data:image/")) {
          const [, meta, data2 = ""] = src.match(/^data:([^;,]+)(?:;base64)?,(.*)$/i) ?? [];
          if (meta && data2 && data2.length <= maxImageBytes * 1.4) images.push({ data: data2, mimeType: meta, alt: "" });
          continue;
        }
        const response = await fetch(src, { credentials: "omit" });
        const blob = await response.blob();
        if (!response.ok || !blob.type.startsWith("image/") || blob.size > maxImageBytes) continue;
        const data = await new Promise((resolve, reject) => {
          const reader = new FileReader();
          reader.onload = () => resolve(String(reader.result).split(",")[1] || "");
          reader.onerror = () => reject(reader.error);
          reader.readAsDataURL(blob);
        });
        images.push({ data, mimeType: blob.type, alt: "" });
      } catch {
        warnings.push(`Could not fetch image: ${src}`);
      }
    }
  }
  const contentType = document.contentType || "text/html";
  return { title: document.title, url: location.href, contentType, text: options["linksOnly"] ? "" : text, links, images, truncated, warnings };
}

// src/background/service-worker.ts
var defaults = { wsUrl: "ws://127.0.0.1:8788/bridge", token: "" };
var managedTabKey = "managedTabId";
var managedTabId;
var managedTabOperation;
var socket;
var retry = 0;
var reconnectTimer;
var connectInFlight = false;
var heartbeatTimer;
chrome.runtime.onMessage.addListener((message) => {
  if (message?.type === "reconnect") void connect(true);
});
chrome.tabs.onRemoved.addListener((tabId) => {
  if (managedTabId === tabId) {
    managedTabId = void 0;
    void chrome.storage.local.remove(managedTabKey);
  }
});
chrome.tabs.onReplaced.addListener((addedTabId, removedTabId) => {
  if (managedTabId === removedTabId) {
    managedTabId = addedTabId;
    void chrome.storage.local.set({ [managedTabKey]: addedTabId });
  }
});
chrome.runtime.onStartup.addListener(() => void connect());
chrome.runtime.onInstalled.addListener(() => void connect());
void connect();
async function connect(force = false) {
  if (connectInFlight) return;
  if (force && socket) {
    const oldSocket = socket;
    socket = void 0;
    oldSocket.close(1e3, "reconnecting");
  }
  if (socket && (socket.readyState === WebSocket.OPEN || socket.readyState === WebSocket.CONNECTING)) return;
  connectInFlight = true;
  try {
    const { wsUrl, token } = await chrome.storage.local.get(defaults);
    if (!token || !/^ws:\/\/(127\.0\.0\.1|localhost)(?::\d+)?\/bridge$/.test(wsUrl)) return;
    const candidate = new WebSocket(wsUrl);
    socket = candidate;
    candidate.onopen = () => {
      if (socket !== candidate) {
        candidate.close(1e3, "replaced");
        return;
      }
      retry = 0;
      send({ type: "hello", token }, candidate);
      if (heartbeatTimer) clearInterval(heartbeatTimer);
      heartbeatTimer = setInterval(() => send({ type: "heartbeat" }, candidate), 15e3);
    };
    candidate.onmessage = (event) => {
      if (socket !== candidate) return;
      try {
        void receive(JSON.parse(event.data));
      } catch {
      }
    };
    candidate.onclose = () => {
      if (heartbeatTimer) {
        clearInterval(heartbeatTimer);
        heartbeatTimer = void 0;
      }
      if (socket === candidate) {
        socket = void 0;
        schedule();
      }
    };
    candidate.onerror = () => {
      if (socket === candidate) candidate.close();
    };
  } finally {
    connectInFlight = false;
  }
}
function schedule() {
  if (reconnectTimer) clearTimeout(reconnectTimer);
  const delay = Math.min(3e4, 1e3 * 2 ** retry++);
  reconnectTimer = setTimeout(() => {
    reconnectTimer = void 0;
    void connect();
  }, delay);
}
function send(message, target = socket) {
  if (target?.readyState === WebSocket.OPEN) target.send(JSON.stringify(message));
}
async function receive(envelope) {
  if (envelope.type === "heartbeat" || envelope.type === "heartbeat_ack") {
    if (envelope.type === "heartbeat") send({ type: "heartbeat_ack" });
    return;
  }
  if (envelope.type !== "command" || !envelope.requestId) return;
  try {
    const command = envelope.payload;
    const payload = await execute(command);
    send({ type: "result", requestId: envelope.requestId, payload });
  } catch (error) {
    send({ type: "error", requestId: envelope.requestId, error: error instanceof Error ? error.message : "browser command failed" });
  }
}
async function execute(command) {
  switch (command.name) {
    case "list_tabs":
      return (await chrome.tabs.query({})).map((t) => ({ id: t.id, title: t.title ?? "", url: t.url ?? "", active: !!t.active }));
    case "open_tab":
      return openTab(command.args);
    case "open_link":
      return openLink(command.args);
    case "get_page":
      return extract(command.args, false);
    case "get_links":
      return extract(command.args, true);
    default:
      throw new Error(`Unsupported command: ${command.name}`);
  }
}
async function openTab(args) {
  const url = String(args["url"] ?? "").trim();
  if (!/^https?:\/\//i.test(url)) throw new Error("url must be an HTTP(S) URL");
  const active = args["active"] !== false;
  const operation = managedTabOperation ?? openOrUpdateManagedTab(url, active);
  managedTabOperation = operation;
  try {
    const tab = await operation;
    return { id: tab.id, title: tab.title ?? "", url: tab.url ?? url, active: !!tab.active };
  } finally {
    if (managedTabOperation === operation) managedTabOperation = void 0;
  }
}
async function openOrUpdateManagedTab(url, active) {
  const stored = await chrome.storage.local.get(managedTabKey);
  managedTabId = typeof stored[managedTabKey] === "number" ? stored[managedTabKey] : managedTabId;
  let tab;
  if (managedTabId !== void 0) {
    try {
      tab = await chrome.tabs.get(managedTabId);
    } catch {
      managedTabId = void 0;
      await chrome.storage.local.remove(managedTabKey);
    }
  }
  if (tab?.id !== void 0) {
    const updated = await chrome.tabs.update(tab.id, { url, active });
    if (!updated || updated.id === void 0) throw new Error("Chrome did not return the updated managed tab");
    return waitForTabReady(updated.id);
  }
  tab = await chrome.tabs.create({ url, active });
  if (tab.id === void 0) throw new Error("Chrome did not return a tab ID");
  managedTabId = tab.id;
  await chrome.storage.local.set({ [managedTabKey]: managedTabId });
  return waitForTabReady(tab.id);
}
async function waitForTabReady(tabId) {
  const deadline = Date.now() + 9e4;
  while (Date.now() < deadline) {
    try {
      const tab = await chrome.tabs.get(tabId);
      if (tab.status === "complete") return tab;
    } catch {
      throw new Error("Managed tab was closed while loading");
    }
    await new Promise((resolve) => setTimeout(resolve, 250));
  }
  throw new Error("Timed out waiting for managed tab navigation to complete");
}
async function openLink(args) {
  const url = String(args["url"] ?? "").trim();
  if (!/^https?:\/\//i.test(url)) throw new Error("url must be an HTTP(S) URL");
  let tab = await openOrUpdateManagedTab(url, true);
  if (tab.id === void 0) throw new Error("Chrome did not return a tab ID");
  const tabId = tab.id;
  let info;
  let lastError;
  for (let attempt = 0; attempt < 2; attempt++) {
    try {
      const result = await chrome.scripting.executeScript({ target: { tabId }, func: () => ({ url: location.href, title: document.title, contentType: document.contentType || "text/html" }) });
      info = result[0]?.result;
      if (info) break;
    } catch (error) {
      lastError = error;
      const message = error instanceof Error ? error.message : String(error);
      if (!message.includes("Frame with ID 0 is showing error page") || attempt > 0) break;
      await new Promise((resolve) => setTimeout(resolve, 15e3));
      await chrome.tabs.reload(tabId, { bypassCache: true });
      tab = await waitForTabReady(tabId);
    }
  }
  if (!info) {
    const current = await chrome.tabs.get(tabId);
    return {
      url: current.url ?? url,
      contentType: "markdown",
      title: current.title ?? "",
      markdown: `# Page is unavailable\\n\\nRequested URL: ${url}\\n\\nThe browser navigated to an error page and Chrome did not allow content extraction after one retry.\\n\\nError: ${lastError instanceof Error ? lastError.message : String(lastError)}`
    };
  }
  if (info.contentType.startsWith("image/")) {
    const image = await chrome.scripting.executeScript({ target: { tabId }, func: async () => {
      const response = await fetch(location.href, { credentials: "omit" });
      const blob = await response.blob();
      if (blob.size > 5 * 1024 * 1024) throw new Error("image exceeds 5 MiB limit");
      const data = await new Promise((resolve, reject) => {
        const reader = new FileReader();
        reader.onload = () => resolve(String(reader.result).split(",")[1] || "");
        reader.onerror = () => reject(reader.error);
        reader.readAsDataURL(blob);
      });
      return { url: location.href, contentType: "image", mimeType: blob.type, data };
    } });
    return image[0]?.result;
  }
  let content;
  try {
    const page = await chrome.scripting.executeScript({ target: { tabId }, func: pageExtractor, args: [{ includeImages: false, maxTextChars: 2e5, maxLinks: 500 }] });
    content = page[0]?.result;
  } catch (error) {
    const current = await chrome.tabs.get(tabId);
    return {
      url: current.url ?? info.url,
      contentType: "markdown",
      title: current.title ?? info.title,
      markdown: `# Page is unavailable\\n\\nRequested URL: ${url}\\n\\nThe browser navigated to an error page and Chrome did not allow content extraction.\\n\\nError: ${error instanceof Error ? error.message : String(error)}`
    };
  }
  if (!content) throw new Error("No page content received");
  return { url: content.url, contentType: "markdown", markdown: content.text, title: content.title };
}
async function extract(args, linksOnly) {
  const tab = await targetTab(args);
  if (!tab.id) throw new Error("Target tab has no ID");
  if (!/^https?:/i.test(tab.url ?? "")) throw new Error("Chrome cannot extract this page type");
  const result = await chrome.scripting.executeScript({ target: { tabId: tab.id }, func: pageExtractor, args: [{ linksOnly, ...args }] });
  if (!result[0]?.result) throw new Error("No document result received");
  return result[0].result;
}
async function targetTab(args) {
  if (typeof args["tabId"] === "number") {
    const tab = await chrome.tabs.get(args["tabId"]);
    return tab;
  }
  if (typeof args["url"] === "string") {
    const tabs2 = await chrome.tabs.query({ url: args["url"] });
    if (tabs2[0]) return tabs2[0];
    throw new Error("No open tab exactly matches URL");
  }
  const stored = await chrome.storage.local.get(managedTabKey);
  const id = typeof stored[managedTabKey] === "number" ? stored[managedTabKey] : managedTabId;
  if (id !== void 0) {
    try {
      return await chrome.tabs.get(id);
    } catch {
      managedTabId = void 0;
      await chrome.storage.local.remove(managedTabKey);
    }
  }
  const tabs = await chrome.tabs.query({ active: true, lastFocusedWindow: true });
  if (!tabs[0]) throw new Error("No active tab");
  return tabs[0];
}
