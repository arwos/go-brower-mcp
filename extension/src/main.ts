import { bootstrapApplication } from '@angular/platform-browser';
import { Component, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';

const defaults = { wsUrl: 'ws://127.0.0.1:8788/bridge', token: '' };
@Component({
  selector: 'app-root', standalone: true, imports: [FormsModule],
  template: `<main><h1>Local Browser MCP</h1><p>Connect Chrome to the local MCP server.</p><label>WebSocket URL<input [(ngModel)]="wsUrl"></label><label>Bridge token<input [(ngModel)]="token" type="text" autocomplete="off"></label><button (click)="save()">Save and connect</button><p class="status">{{ status() }}</p><small>The token is stored locally in this Chrome profile and remains available in this field.</small></main>`,
  styles: [`main{width:320px;padding:16px;font:14px system-ui;color:#1c1b1f}h1{font-size:20px;margin:0 0 8px}label{display:block;margin:12px 0 4px;font-weight:600}input{display:block;box-sizing:border-box;width:100%;margin-top:4px;padding:8px;border:1px solid #777;border-radius:4px}button{margin-top:16px;background:#155eef;color:#fff;border:0;border-radius:4px;padding:9px 12px;font-weight:600}.status{min-height:20px;color:#155eef}small{color:#5f6368}`]
})
export class App {
  wsUrl = defaults.wsUrl; token = ''; status = signal('Loading settings…');
  async ngOnInit() { const values = await chrome.storage.local.get(defaults); this.wsUrl = values.wsUrl; this.token = values.token; this.status.set('Ready to connect'); }
  async save() { const wsUrl = this.wsUrl.trim(); const token = this.token.trim(); if (!token || !/^ws:\/\/(127\.0\.0\.1|localhost)(?::\d+)?\/bridge$/.test(wsUrl)) { this.status.set('Enter a valid loopback WebSocket URL and token.'); return; } await chrome.storage.local.set({ wsUrl, token }); this.wsUrl = wsUrl; this.token = token; await chrome.runtime.sendMessage({type:'reconnect'}); this.status.set('Saved. Connection requested.'); }
}
bootstrapApplication(App);
