import { pageExtractor as markdownPageExtractor } from './page-extractor';

type Envelope = { type: string; requestId?: string; token?: string; payload?: unknown; error?: string };
type Command = { name: string; args: Record<string, unknown> };
const defaults = { wsUrl: 'ws://127.0.0.1:8788/bridge', token: '' };
const managedTabKey = 'managedTabId';
let managedTabId: number | undefined;
let managedTabOperation: Promise<chrome.tabs.Tab> | undefined;
let socket: WebSocket | undefined;
let retry = 0;
let reconnectTimer: ReturnType<typeof setTimeout> | undefined;
let connectInFlight = false;
let heartbeatTimer: ReturnType<typeof setInterval> | undefined;

chrome.runtime.onMessage.addListener((message) => { if (message?.type === 'reconnect') void connect(true); });
chrome.tabs.onRemoved.addListener((tabId) => { if (managedTabId === tabId) { managedTabId = undefined; void chrome.storage.local.remove(managedTabKey); } });
chrome.tabs.onReplaced.addListener((addedTabId, removedTabId) => { if (managedTabId === removedTabId) { managedTabId = addedTabId; void chrome.storage.local.set({[managedTabKey]: addedTabId}); } });
chrome.runtime.onStartup.addListener(() => void connect());
chrome.runtime.onInstalled.addListener(() => void connect());
void connect();

async function connect(force = false) {
  if (connectInFlight) return;
  if (force && socket) {
    const oldSocket = socket;
    socket = undefined;
    oldSocket.close(1000, 'reconnecting');
  }
  if (socket && (socket.readyState === WebSocket.OPEN || socket.readyState === WebSocket.CONNECTING)) return;

  connectInFlight = true;
  try {
    const { wsUrl, token } = await chrome.storage.local.get(defaults);
    if (!token || !/^ws:\/\/(127\.0\.0\.1|localhost)(?::\d+)?\/bridge$/.test(wsUrl)) return;

    const candidate = new WebSocket(wsUrl);
    socket = candidate;
    candidate.onopen = () => {
      if (socket !== candidate) { candidate.close(1000, 'replaced'); return; }
      retry = 0;
      send({type:'hello', token}, candidate);
      if (heartbeatTimer) clearInterval(heartbeatTimer);
      heartbeatTimer = setInterval(() => send({type:'heartbeat'}, candidate), 15_000);
    };
    candidate.onmessage = event => {
      if (socket !== candidate) return;
      try { void receive(JSON.parse(event.data)); } catch { /* ignore invalid local input */ }
    };
    candidate.onclose = () => {
      if (heartbeatTimer) { clearInterval(heartbeatTimer); heartbeatTimer = undefined; }
      if (socket === candidate) { socket = undefined; schedule(); }
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
  const delay = Math.min(30_000, 1_000 * 2 ** retry++);
  reconnectTimer = setTimeout(() => { reconnectTimer = undefined; void connect(); }, delay);
}
function send(message: Envelope, target = socket) {
  if (target?.readyState === WebSocket.OPEN) target.send(JSON.stringify(message));
}
async function receive(envelope: Envelope) {
  if (envelope.type === 'heartbeat' || envelope.type === 'heartbeat_ack') { if (envelope.type === 'heartbeat') send({type:'heartbeat_ack'}); return; }
  if (envelope.type !== 'command' || !envelope.requestId) return;
  try { const command = envelope.payload as Command; const payload = await execute(command); send({type:'result',requestId:envelope.requestId,payload}); }
  catch (error) { send({type:'error',requestId:envelope.requestId,error:error instanceof Error ? error.message : 'browser command failed'}); }
}
async function execute(command: Command): Promise<unknown> {
  switch(command.name) {
    case 'list_tabs': return (await chrome.tabs.query({})).map(t => ({id:t.id,title:t.title ?? '',url:t.url ?? '',active:!!t.active}));
    case 'open_tab': return openTab(command.args);
    case 'open_link': return openLink(command.args);
    case 'get_page': return extract(command.args, false);
    case 'get_links': return extract(command.args, true);
    default: throw new Error(`Unsupported command: ${command.name}`);
  }
}
async function openTab(args: Record<string, unknown>) {
  const url = String(args['url'] ?? '').trim();
  if (!/^https?:\/\//i.test(url)) throw new Error('url must be an HTTP(S) URL');
  const active = args['active'] !== false;
  const operation = managedTabOperation ?? openOrUpdateManagedTab(url, active);
  managedTabOperation = operation;
  try {
    const tab = await operation;
    return {id:tab.id,title:tab.title ?? '',url:tab.url ?? url,active:!!tab.active};
  } finally {
    if (managedTabOperation === operation) managedTabOperation = undefined;
  }
}
async function openOrUpdateManagedTab(url: string, active: boolean): Promise<chrome.tabs.Tab> {
  const stored = await chrome.storage.local.get(managedTabKey);
  managedTabId = typeof stored[managedTabKey] === 'number' ? stored[managedTabKey] : managedTabId;
  let tab: chrome.tabs.Tab | undefined;
  if (managedTabId !== undefined) {
    try { tab = await chrome.tabs.get(managedTabId); }
    catch { managedTabId = undefined; await chrome.storage.local.remove(managedTabKey); }
  }
  if (tab?.id !== undefined) {
    const updated = await chrome.tabs.update(tab.id, {url, active});
    if (!updated || updated.id === undefined) throw new Error('Chrome did not return the updated managed tab');
    return waitForTabReady(updated.id);
  }
  tab = await chrome.tabs.create({url, active});
  if (tab.id === undefined) throw new Error('Chrome did not return a tab ID');
  managedTabId = tab.id;
  await chrome.storage.local.set({[managedTabKey]: managedTabId});
  return waitForTabReady(tab.id);
}
async function waitForTabReady(tabId: number): Promise<chrome.tabs.Tab> {
  const deadline = Date.now() + 90_000;
  while (Date.now() < deadline) {
    try {
      const tab = await chrome.tabs.get(tabId);
      if (tab.status === 'complete') return tab;
    } catch {
      throw new Error('Managed tab was closed while loading');
    }
    await new Promise(resolve => setTimeout(resolve, 250));
  }
  throw new Error('Timed out waiting for managed tab navigation to complete');
}
async function openLink(args: Record<string, unknown>) {
  const url = String(args['url'] ?? '').trim();
  if (!/^https?:\/\//i.test(url)) throw new Error('url must be an HTTP(S) URL');
  let tab = await openOrUpdateManagedTab(url, true);
  if (tab.id === undefined) throw new Error('Chrome did not return a tab ID');
  const tabId = tab.id;
  let info: {url: string, title: string, contentType: string} | undefined;
  let lastError: unknown;
  for (let attempt = 0; attempt < 2; attempt++) {
    try {
      const result = await chrome.scripting.executeScript({target: {tabId: tabId}, func: () => ({url: location.href, title: document.title, contentType: document.contentType || 'text/html'})});
      info = result[0]?.result;
      if (info) break;
    } catch (error) {
      lastError = error;
      const message = error instanceof Error ? error.message : String(error);
      if (!message.includes('Frame with ID 0 is showing error page') || attempt > 0) break;
      await new Promise(resolve => setTimeout(resolve, 15_000));
      await chrome.tabs.reload(tabId, {bypassCache: true});
      tab = await waitForTabReady(tabId);
    }
  }
  if (!info) {
    const current = await chrome.tabs.get(tabId);
    return {
      url: current.url ?? url,
      contentType: 'markdown',
      title: current.title ?? '',
      markdown: `# Page is unavailable\\n\\nRequested URL: ${url}\\n\\nThe browser navigated to an error page and Chrome did not allow content extraction after one retry.\\n\\nError: ${lastError instanceof Error ? lastError.message : String(lastError)}`,
    };
  }
  if (info.contentType.startsWith('image/')) {
    const image = await chrome.scripting.executeScript({target: {tabId: tabId}, func: async () => { const response = await fetch(location.href, {credentials: 'omit'}); const blob = await response.blob(); if (blob.size > 5 * 1024 * 1024) throw new Error('image exceeds 5 MiB limit'); const data = await new Promise<string>((resolve, reject) => { const reader = new FileReader(); reader.onload = () => resolve(String(reader.result).split(',')[1] || ''); reader.onerror = () => reject(reader.error); reader.readAsDataURL(blob); }); return {url: location.href, contentType: 'image', mimeType: blob.type, data}; }});
    return image[0]?.result;
  }
  let content: {url: string, title: string, text: string} | undefined;
  try {
    const page = await chrome.scripting.executeScript({target: {tabId: tabId}, func: markdownPageExtractor, args: [{includeImages: false, maxTextChars: 200_000, maxLinks: 500}]});
    content = page[0]?.result;
  } catch (error) {
    const current = await chrome.tabs.get(tabId);
    return {
      url: current.url ?? info.url,
      contentType: 'markdown',
      title: current.title ?? info.title,
      markdown: `# Page is unavailable\\n\\nRequested URL: ${url}\\n\\nThe browser navigated to an error page and Chrome did not allow content extraction.\\n\\nError: ${error instanceof Error ? error.message : String(error)}`,
    };
  }
  if (!content) throw new Error('No page content received');
  return {url: content.url, contentType: 'markdown', markdown: content.text, title: content.title};
}
async function extract(args: Record<string, unknown>, linksOnly: boolean) { const tab=await targetTab(args); if(!tab.id) throw new Error('Target tab has no ID'); if(!/^https?:/i.test(tab.url ?? '')) throw new Error('Chrome cannot extract this page type'); const result=await chrome.scripting.executeScript({target:{tabId:tab.id},func: markdownPageExtractor,args:[{linksOnly,...args}]}); if(!result[0]?.result) throw new Error('No document result received'); return result[0].result; }
async function targetTab(args: Record<string, unknown>) { if (typeof args['tabId']==='number') { const tab=await chrome.tabs.get(args['tabId']);return tab; } if(typeof args['url']==='string'){const tabs=await chrome.tabs.query({url:args['url']});if(tabs[0]) return tabs[0];throw new Error('No open tab exactly matches URL');} const stored = await chrome.storage.local.get(managedTabKey); const id = typeof stored[managedTabKey] === 'number' ? stored[managedTabKey] : managedTabId; if (id !== undefined) { try { return await chrome.tabs.get(id); } catch { managedTabId = undefined; await chrome.storage.local.remove(managedTabKey); } } const tabs=await chrome.tabs.query({active:true,lastFocusedWindow:true});if(!tabs[0]) throw new Error('No active tab');return tabs[0]; }
async function pageExtractor(options: Record<string, unknown>) {
 const clean=(s:string)=>s.replace(/\s+/g,' ').trim(); const maxText=Math.min(Number(options['maxTextChars']??50_000),200_000);const maxLinks=Math.min(Number(options['maxLinks']??options['limit']??100),500); const maxImages=Math.min(Number(options['maxImages']??5),10);const maxImageBytes=1_500_000; const root=document.body.cloneNode(true) as HTMLElement;root.querySelectorAll('script,style,noscript,template,svg').forEach(x=>x.remove());let text=clean(root.innerText||'');const truncated=text.length>maxText;text=text.slice(0,maxText); const seen=new Set<string>();const links=Array.from(document.querySelectorAll<HTMLAnchorElement>('a[href]')).map(a=>({text:clean(a.innerText)||clean(a.getAttribute('aria-label')||'')||a.href,url:a.href})).filter(l=>/^https?:/i.test(l.url)&&!seen.has(l.url)&&(seen.add(l.url),true)).filter(l=>!options['domain']||new URL(l.url).hostname===options['domain']).slice(0,maxLinks); const warnings:string[]=[];const images:{data:string,mimeType:string,alt:string}[]=[];if(options['includeImages']){const srcs=[...new Set(Array.from(document.images).map(i=>i.currentSrc||i.src).filter(src=>/^https?:|^data:image\//i.test(src)))].slice(0,maxImages);for(const src of srcs){try{if(src.startsWith('data:image/')){const [,meta,data='']=src.match(/^data:([^;,]+)(?:;base64)?,(.*)$/i)??[];if(meta&&data&&data.length<=maxImageBytes*1.4)images.push({data,mimeType:meta,alt:''});continue;}const response=await fetch(src,{credentials:'omit'});const blob=await response.blob();if(!response.ok||!blob.type.startsWith('image/')||blob.size>maxImageBytes)continue;const data=await new Promise<string>((resolve,reject)=>{const r=new FileReader();r.onload=()=>resolve(String(r.result).split(',')[1]||'');r.onerror=()=>reject(r.error);r.readAsDataURL(blob);});images.push({data,mimeType:blob.type,alt:''});}catch{warnings.push(`Could not fetch image: ${src}`)}}}return {title:document.title,url:location.href,text:options['linksOnly']?'':text,links,images,truncated,warnings};
}
