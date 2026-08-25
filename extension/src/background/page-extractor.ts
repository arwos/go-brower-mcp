export async function pageExtractor(options: Record<string, unknown>) {
  const clean = (value: string) => value.replace(/[ \t\f\v]+/g, ' ').replace(/\n{3,}/g, '\n\n').trim();
  const escape = (value: string) => value.replace(/[\\`*_\[\]<>]/g, '\\$&');
  const cleanLinkText = (value: string) => clean(value).replace(/\s*\n\s*/g, ' ').replace(/\s+/g, ' ').trim();
  const markdownLink = (label: string, href: string) => {
    const url = new URL(href, location.href);
    if (url.protocol !== 'http:' && url.protocol !== 'https:') return cleanLinkText(label);
    const safeLabel = cleanLinkText(label) || url.href;
    return `[${escape(safeLabel)}](${url.href.replace(/[()\\]/g, '\\$&')})`;
  };
  const maxText = Math.min(Number(options['maxTextChars'] ?? 50_000), 200_000);
  const maxLinks = Math.min(Number(options['maxLinks'] ?? options['limit'] ?? 100), 500);
  const maxImages = Math.min(Number(options['maxImages'] ?? 5), 10);
  const maxImageBytes = 1_500_000;
  const root = document.body.cloneNode(true) as HTMLElement;
  root.querySelectorAll('script,style,noscript,template,svg,[hidden],[aria-hidden="true"]').forEach(x => x.remove());
  const inline = (node: Node): string => {
    if (node.nodeType === Node.TEXT_NODE) return escape(node.textContent ?? '');
    if (node.nodeType !== Node.ELEMENT_NODE) return '';
    const element = node as HTMLElement;
    const value = Array.from(element.childNodes).map(inline).join('');
    if (element.tagName === 'A') { const href = (element as HTMLAnchorElement).href; return /^https?:/i.test(href) ? markdownLink(element.textContent ?? value, href) : value; }
    if (element.tagName === 'STRONG' || element.tagName === 'B') return value ? `**${value}**` : '';
    if (element.tagName === 'EM' || element.tagName === 'I') return value ? `*${value}*` : '';
    if (element.tagName === 'CODE') return value ? `\`${value}\`` : '';
    if (element.tagName === 'BR') return '\n';
    return value;
  };
  const block = (node: Node, depth = 0): string => {
    if (node.nodeType === Node.TEXT_NODE) return escape(node.textContent ?? '');
    if (node.nodeType !== Node.ELEMENT_NODE) return '';
    const element = node as HTMLElement;
    if (element.tagName === 'TABLE') {
      const rows = Array.from((element as HTMLTableElement).rows).map(row => Array.from(row.cells).map(cell => clean(inline(cell)).replace(/\|/g, '\\|'))).filter(row => row.some(Boolean));
      if (!rows.length) return '';
      const width = Math.max(...rows.map(row => row.length));
      const normalized = rows.map(row => [...row, ...Array(width - row.length).fill('')]);
      return [`| ${normalized[0].join(' | ')} |`, `| ${normalized[0].map(() => '---').join(' | ')} |`, ...normalized.slice(1).map(row => `| ${row.join(' | ')} |`)].join('\n') + '\n\n';
    }
    const content = Array.from(element.childNodes).map(child => block(child, depth + 1)).join('');
    const value = inline(element);
    if (/^H[1-6]$/.test(element.tagName)) return `${'#'.repeat(Number(element.tagName[1]))} ${clean(value)}\n\n`;
    if (element.tagName === 'LI') return `${'  '.repeat(Math.max(0, depth - 2))}- ${clean(value)}\n`;
    if (element.tagName === 'PRE') return `\n\n\`\`\`\n${element.textContent?.trim() ?? ''}\n\`\`\`\n\n`;
    if (element.tagName === 'HR') return '\n\n---\n\n';
    if (['P','BLOCKQUOTE','ARTICLE','SECTION','MAIN','HEADER','FOOTER','FIGURE','FIGCAPTION','DIV','ASIDE','NAV','FORM','UL','OL','DL','DD','DT','ADDRESS'].includes(element.tagName)) return `${clean(content || value)}\n\n`;
    return value;
  };
  let text = clean(block(root));
  const truncated = text.length > maxText;
  text = text.slice(0, maxText);
  const seen = new Set<string>();
  const links = Array.from(document.querySelectorAll<HTMLAnchorElement>('a[href]')).map(a => ({text: clean(a.innerText) || clean(a.getAttribute('aria-label') || '') || a.href, url: a.href})).filter(l => /^https?:/i.test(l.url) && !seen.has(l.url) && (seen.add(l.url), true)).filter(l => !options['domain'] || new URL(l.url).hostname === options['domain']).slice(0, maxLinks);
  const warnings: string[] = [];
  const images: {data: string, mimeType: string, alt: string}[] = [];
  if (options['includeImages']) { const srcs = [...new Set(Array.from(document.images).map(i => i.currentSrc || i.src).filter(src => /^https?:|^data:image\//i.test(src)))].slice(0, maxImages); for (const src of srcs) { try { if (src.startsWith('data:image/')) { const [, meta, data = ''] = src.match(/^data:([^;,]+)(?:;base64)?,(.*)$/i) ?? []; if (meta && data && data.length <= maxImageBytes * 1.4) images.push({data, mimeType: meta, alt: ''}); continue; } const response = await fetch(src, {credentials: 'omit'}); const blob = await response.blob(); if (!response.ok || !blob.type.startsWith('image/') || blob.size > maxImageBytes) continue; const data = await new Promise<string>((resolve, reject) => { const reader = new FileReader(); reader.onload = () => resolve(String(reader.result).split(',')[1] || ''); reader.onerror = () => reject(reader.error); reader.readAsDataURL(blob); }); images.push({data, mimeType: blob.type, alt: ''}); } catch { warnings.push(`Could not fetch image: ${src}`); } } }
  const contentType = document.contentType || 'text/html';
  return {title: document.title, url: location.href, contentType, text: options['linksOnly'] ? '' : text, links, images, truncated, warnings};
}

