# Локальный стенд

## Адреса

Все публичные адреса задаются в корневом `.env`. Значения по умолчанию:

- клиентская HTML-страница с SDK: `http://client.localhost:4174`;
- рабочая админка: `http://backlog.localhost:4173`;
- API и MCP: `http://api.localhost:18080`;
- OIDC/Keycloak: `http://identity.localhost:8081`.

Домены `*.localhost` разрешаются современными браузерами в `127.0.0.1`; файл `hosts` не требуется.

## Запуск

```sh
cp .env.example .env
# Для локальной разработки значения change-me допустимы только после замены
# локальных паролей из примера на собственные.
docker compose up --build -d
docker compose ps
```

Локальные демонстрационные пользователи:

| Роль | Логин | Пароль |
|---|---|---|
| владелец и администратор платформы | `owner` | `owner-password` |
| менеджер | `triager` | `triager-password` |
| клиент | `reporter` | `reporter-password` |

Они находятся только в `infra/keycloak/realm.local.json`. Production realm не содержит пользователей и запрещает password grant.

## Основной сценарий

1. Открыть `http://client.localhost:4174`.
2. Нажать **Backlog**, войти клиентом и включить **Поставить тикет**.
3. Нажать на элемент страницы, заполнить стикер и создать тикет.
4. Открыть `http://backlog.localhost:4173`, войти владельцем.
5. В **Беклоге** дополнить техническое задание и нажать **Передать агенту**.
6. Создать PAT в **Администрирование → MCP и агенты** и подключить агент к `http://api.localhost:18080/mcp`.
7. Агент читает тикет и вложения, claim-ит работу и отправляет результат на утверждение.
8. Владелец принимает результат или возвращает его на доработку с обязательным комментарием.

## Создание клиента вручную

Только точная пара `OIDC issuer|subject` из `PLATFORM_ADMIN_SUBJECTS` видит форму **Новый клиент**. Форма создаёт tenant и первый проект. Затем:

1. добавить точный origin сайта клиента;
2. создать приглашения пользователей;
3. скопировать SDK snippet;
4. при необходимости создать PAT для MCP.

`projectKey` публичный. PAT и bearer token запрещено помещать в HTML, URL, localStorage или sessionStorage.

## Проверка сервисов

```sh
curl http://api.localhost:18080/livez
curl http://api.localhost:18080/healthz
curl http://api.localhost:18080/readyz
curl -I http://backlog.localhost:4173/sdk/backlog-killer.js
```

SDK должен возвращать JavaScript, а не SPA HTML.
