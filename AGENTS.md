# Контекст проекта: Local Browser MCP

## Назначение

Проект предоставляет локальный MCP-сервер для управления Chrome/Chromium через расширение браузера:

- открытие вкладок;
- выполнение поисковых запросов;
- список вкладок;
- извлечение видимого текста страницы, ссылок и изображений;
- возврат текста в Markdown и изображений как MCP `image` content с base64.

Все компоненты рассчитаны на локальный запуск. Сервер запрещает запуск слушателей не на loopback-интерфейсе.

## Структура

```text
.
├── README.md                    # Инструкции запуска и безопасность
├── server/                      # Go 1.26 MCP сервер
│   ├── cmd/browser-mcp/main.go  # CLI, запуск HTTP/stdio/WS серверов
│   └── internal/
│       ├── bridge/              # WebSocket-протокол и hub соединения расширения
│       └── mcp/                 # JSON-RPC/MCP ядро, инструменты и транспорты
└── extension/                   # Angular 22 Chrome MV3 extension
    ├── public/manifest.json     # Manifest V3
    ├── src/main.ts              # Angular popup UI
    └── src/background/
        ├── service-worker.ts    # WebSocket bridge и Chrome API
        └── page-extractor.ts    # HTML → Markdown и извлечение изображений
```

## Технологии

- Go **1.26**;
- `github.com/coder/websocket` для канала server ↔ extension;
- Angular **22**;
- Chrome Extension Manifest V3;
- pnpm / Node.js для расширения.

## MCP-сервер

Бинарник: `server/cmd/browser-mcp`.

Режимы:

```text
-mode=stdio|http|both
```

По умолчанию `both`.

Адреса по умолчанию:

```text
HTTP MCP:        http://127.0.0.1:8787/mcp
WebSocket bridge: ws://127.0.0.1:8788/bridge
```

Основные параметры:

```text
-http-token=<secret>
-bridge-token=<secret>
-http-addr=127.0.0.1:8787
-ws-addr=127.0.0.1:8788
-request-timeout=2m
-search-engine=google|bing|duckduckgo
-allowed-origin=http://127.0.0.1:8080
```

Если токены не переданы, они генерируются и печатаются только в stderr/log. Не выводите и не коммитьте реальные токены.

### MCP-инструменты

- `browser_list_tabs`
- `browser_open_tab`
- `browser_search`
- `browser_get_page`
- `browser_get_links`
- `web_search` (server-configured search engine)
- `web_get_link` (final URL after redirects and Markdown/image content; HTTP(S) only)

Инструменты определены в `server/internal/mcp/server.go`.

### Транспорты

- **stdio**: JSON-RPC сообщения построчно через stdin/stdout; логи строго в stderr.
- **HTTP**: JSON-RPC `POST /mcp`, `Content-Type: application/json`, bearer-аутентификация. Реализация находится в `server/internal/mcp/transport.go`.
- **WebSocket**: предназначен только для расширения; необходимо `hello`-сообщение с bridge token.

При изменении MCP-версии, сессий или полноценной поддержки SSE/потоковых ответов обязательно сверяйтесь с актуальной спецификацией MCP Streamable HTTP и добавляйте интеграционные тесты.

## Chrome-расширение

Расширение не требует отдельного dev-сервера. Для установки в Chrome используется production output:

```text
extension/dist/browser
```

Service worker создаётся отдельной esbuild-командой, потому что Angular application build собирает popup, но не MV3 worker:

```bash
node_modules/.bin/esbuild src/background/service-worker.ts \
  --bundle --format=esm --target=es2022 \
  --outfile=dist/browser/service-worker.js
```

`extension/package.json` уже содержит этот шаг в `pnpm build`.

### Важные детали расширения

- Настройки WebSocket и bridge token сохраняются постоянно в `chrome.storage.local`; токен снова отображается в popup.
- `managedTabId` в `chrome.storage.local` указывает на единственную вкладку для server-side open/search/link; закрытие вкладки очищает ID.
- Поисковик выбирается только Go-сервером через `-search-engine`/`BROWSER_MCP_SEARCH_ENGINE`; расширение получает готовый HTTP(S) URL и не содержит provider map.
- Для WebSocket в расширении допускаются только `127.0.0.1` и `localhost`; IPv6 `[::1]` намеренно не используется, так как Chrome отклоняет его в `connect-src` CSP manifest.
- Service worker автоматически переподключается с exponential backoff.
- Извлечение выполняется через `chrome.scripting.executeScript`.
- Для обычного извлечения страницы есть ограничения: максимум 5 изображений по умолчанию, максимум 1.5 MB на изображение; `web_get_link` ограничивает одно возвращаемое изображение 5 MiB; CORS/CSP-ошибки возвращаются как warnings.
- Для `chrome://`, Chrome Web Store, PDF и других закрытых контекстов извлечение ожидаемо может завершиться контролируемой ошибкой.

При добавлении новых browser-команд изменяйте синхронно:

1. определение MCP tool и JSON Schema в `server/internal/mcp/server.go`;
2. WebSocket command contract в `server/internal/bridge/protocol.go` (включая `open_link` для `web_get_link`);
3. обработчик `execute()` в `extension/src/background/service-worker.ts`;
4. README и тесты.

## Сборка и тесты

### Go

```bash
cd server
GOCACHE=$PWD/.cache/go-build GOMODCACHE=$PWD/.cache/go-mod go test ./...
GOCACHE=$PWD/.cache/go-build GOMODCACHE=$PWD/.cache/go-mod go vet ./...
GOCACHE=$PWD/.cache/go-build GOMODCACHE=$PWD/.cache/go-mod go build ./cmd/browser-mcp
```

В окружении DSH глобальные Go-кэши могут быть read-only. Используйте кэши внутри `server/.cache`, как в командах выше.

### Angular extension

```bash
cd extension
pnpm install
pnpm build
```

В DSH pnpm может запрещать lifecycle scripts или пытаться открыть защищённую state DB. Если `pnpm build` не срабатывает после установки, используйте уже установленные локальные бинарники:

```bash
node_modules/.bin/ng build
node_modules/.bin/esbuild src/background/service-worker.ts \
  --bundle --format=esm --target=es2022 \
  --outfile=dist/browser/service-worker.js
```

Angular 22.1.x в текущем lockfile требует TypeScript 6.0.x; не понижайте `typescript` до 5.9.x.

## Безопасность

- Не меняйте loopback-only проверку без прямого требования пользователя и отдельной модели аутентификации.
- Не добавляйте `file:`, `javascript:` или иные опасные URL-схемы в открытие вкладок.
- Не сохраняйте bridge token в `localStorage`, manifest или исходниках.
- Не логируйте содержимое страниц, HTTP bearer token или bridge token.
- При повышении лимитов извлечения учитывайте размер MCP-ответа, память service worker и риск утечки конфиденциального содержимого браузера.

## Перед завершением изменений

1. Запустите Go format/test/vet/build.
2. Соберите extension и убедитесь в наличии `extension/dist/browser/manifest.json` и `service-worker.js`.
3. Для изменений транспорта проверьте `tools/list` HTTP-запросом.
4. Обновите `README.md`, если меняются флаги, endpoint, инструменты, permissions или порядок установки.
5. В каждой JSON Schema инструмента поле `required` должно сериализоваться как массив `[]`, даже если обязательных параметров нет; `null` отклоняется DeepSeek Harness.
