# Aegion Admin SPA

The admin SPA is a Vite + React + TypeScript app using **shadcn/ui** primitives and Tailwind v4 tokens.

## UI and theme

- Base palette is pure black/white neutral (shadcn style).
- Semantic status colors are available for:
  - `success`
  - `warning`
  - `destructive`
  - `info`
- Dashboard includes observability signals for:
  - identity/session/operator telemetry
  - module health probes (`/health`, `/health/ready`)
  - user-management coverage.

## Development

```bash
npm install
npm run dev
```

## Build

```bash
npm run lint
npm run build
```

## shadcn MCP

Project-level MCP config is available at:

`../../.vscode/mcp.json`

It uses:

```json
{
  "servers": {
    "shadcn": {
      "command": "npx",
      "args": ["shadcn@latest", "mcp"]
    }
  }
}
```

`components.json` includes the shadcn registry alias `@shadcn`.
