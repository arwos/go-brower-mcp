# Local Browser MCP

Локальный MCP-сервер на **Go 1.26** и Chrome-расширение на **Angular 22**. Расширение подключается только к loopback WebSocket, а MCP-клиенты получают единый набор browser-инструментов по **stdio** и **Streamable HTTP**.

> Браузерные данные не отправляются во внешние сервисы. Сервер по умолчанию слушает только `127.0.0.1`.

## Возможности

- `browser_list_tabs` — список открытых вкладок;
- `browser_open_tab` — открыть или переиспользовать управляемую вкладку с HTTP(S) URL;
- `browser_search` — legacy-алиас поиска через поисковик, заданный при запуске сервера;
- `browser_get_page` — видимый текст страницы и ссылки в Markdown;
- `browser_get_links` — ссылки страницы в Markdown;
- `web_search` — поиск через поисковик, заданный серверным ключом запуска; принимает `query` или массив `queries`, расширение получает только готовый URL;
- `web_get_link` — переход по HTTP(S)-ссылке с возвратом конечного URL после редиректов и Markdown либо изображения base64.

Ответы текста форматируются как MCP `text` content (Markdown). При `includeImages: true` расширение извлекает до 5 изображений (не более 1.5 MB каждое), проверяет MIME-тип `image/*` и возвращает их как MCP `image` content (`mimeType` + base64). WebSocket bridge принимает сообщения размером до 5 MiB. Ограничения CORS/CSP и недоступные ресурсы выдаются как warnings в Markdown-результате.

## Запуск сервера

```bash
cd server
go run ./cmd/browser-mcp -mode both
```

Сервер напечатает **bridge token** для popup расширения и HTTP bearer token. Не публикуйте их и не добавляйте в репозиторий.

Флаги:

```text
-mode=stdio|http|both              # по умолчанию both
-http-addr=127.0.0.1:8787
-ws-addr=127.0.0.1:8788
-http-token=<secret>               # генерируется, если не задан
-bridge-token=<secret>             # генерируется, если не задан
-request-timeout=2m                    # таймаут браузерных команд
-search-engine=google|bing|duckduckgo # по умолчанию google
`  # CORS Origin для браузерных MCP-клиентов
```

Поисковик для `web_search` задаётся только сервером. Для одного запроса используйте `{\"query\":\"...\"}`, для нескольких — `{\"queries\":[\"...\",\"...\"]}`. Результаты нескольких запросов выполняются последовательно в одной управляемой вкладке и объединяются в Markdown. Также доступны переменные окружения `BROWSER_MCP_SEARCH_ENGINE`, `BROWSER_MCP_MODE`, `BROWSER_MCP_HTTP_ADDR`, `BROWSER_MCP_WS_ADDR`, `BROWSER_MCP_HTTP_TOKEN`, `BROWSER_MCP_BRIDGE_TOKEN`.

Переменная `BROWSER_MCP_SEARCH_ENGINE` также поддерживает выбор `google`, `bing` или `duckduckgo`.

## MCP transports

### stdio

Для конфигурации MCP-клиента:

```json
{
  "mcpServers": {
    "local-browser": {
      "command": "/absolute/path/to/browser-mcp",
      "args": ["-mode", "stdio"]
    }
  }
}
```

Протокольный JSON-RPC передаётся только через stdout; диагностика — stderr.

### Streamable HTTP

Endpoint: `POST http://127.0.0.1:8787/mcp`

Заголовки:

```text
Authorization: Bearer <HTTP_TOKEN>
Content-Type: application/json
Accept: application/json
```

Пример инициализации:

```bash
curl http://127.0.0.1:8787/mcp \
  -H "Authorization: Bearer $BROWSER_MCP_HTTP_TOKEN" \
  -H 'Content-Type: application/json' \
  --data '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"curl","version":"1"}}}'
```

HTTP endpoint не принимает запросы без bearer token. Для browser-based MCP-клиентов включён CORS для `http://127.0.0.1:8080`, `http://localhost:8080` и HTTPS-вариантов. Другой Origin можно явно разрешить флагом `-allowed-origin` или переменной `BROWSER_MCP_ALLOWED_ORIGIN`. Текущая реализация покрывает JSON-RPC запросы и ответы Streamable HTTP; для инструментов не требуется серверный поток событий.

## Сборка и установка расширения

```bash
cd extension
pnpm install
pnpm build
```

1. В Chrome откройте `chrome://extensions`.
2. Включите **Developer mode**.
3. Выберите **Load unpacked** и каталог `extension/dist/browser`.
4. Откройте popup *Local Browser MCP*.
5. Оставьте `ws://127.0.0.1:8788/bridge`, вставьте `bridge token` из лога сервера, нажмите **Save and connect**.

Токен хранится постоянно в `chrome.storage.local`, снова отображается в поле popup после закрытия/открытия popup и доступен только локальному профилю Chrome. Серверные операции открытия, поиска и `web_get_link` используют одну управляемую вкладку: если она существует, расширение переиспользует её; если вкладка закрыта, следующая команда создаёт и запоминает новую.

### `web_get_link`

```json
{"url":"https://example.com/redirect"}
```

Команда открывает URL в управляемой вкладке, ждёт завершения навигации, включая редиректы, и возвращает конечный `location.href`. Для HTML-страниц результат содержит Markdown, для изображений — MCP image content с `mimeType` и base64. Поддерживаются только HTTP(S) URL; максимальный размер изображения — 5 MiB.

## Безопасность и границы

- HTTP и bridge listener проверяют loopback address при старте;
- bridge требует handshake с token до получения команд;
- URL для открытия ограничены HTTP(S); `javascript:` и `file:` не выполняются;
- чтение URL допускается только из уже открытой вкладки с точным совпадением; `web_get_link` сначала открывает переданный HTTP(S) URL в управляемой вкладке;
- Chrome системные страницы, PDF, Chrome Web Store и страницы без scripting access возвращают контролируемую ошибку;
- извлечение текста ограничено 50 000 символов, ссылок — 100 (клиент может уменьшать лимиты).

## Makefile: сборка, проверки и локальный запуск

Из корня проекта доступны команды:

```bash
make help              # список целей
make install            # установить зависимости расширения
make build              # собрать Go-сервер и unpacked Chrome extension
make test               # Go-тесты и TypeScript-проверка
make lint               # go vet и TypeScript-проверка
make check              # build + test + lint
make run                # сервер с stdio и HTTP транспортами
make run-http           # только HTTP MCP
make run-stdio          # только stdio MCP
```

`make run*` собирает бинарник в `bin/browser-mcp`, после чего сервер выводит HTTP и bridge токены в stderr. Для подключения расширения используйте `extension/dist/browser`.

## Проверка Go

```bash
cd server
go test ./...
go vet ./...
```
