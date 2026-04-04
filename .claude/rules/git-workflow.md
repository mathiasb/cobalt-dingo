# Git-workflow

## Remotes
| Remote   | URL                                          | Syfte                        |
|----------|----------------------------------------------|------------------------------|
| `origin` | https://gitea.d-ma.be/mathias/coo-agent.git  | Primär – dagligt arbete      |
| `github` | https://github.com/mathiasb/coo-agent.git    | Sekundär – releases/tags     |

## Dagligt arbete → Gitea
```bash
git push origin main          # vanlig push
git push origin feature/xyz   # feature branch
```
GitHub rörs inte vid dagligt arbete.

## Release → GitHub
```bash
git tag v1.2.0 -m "Release v1.2.0: kort beskrivning"
git push origin main          # koden till Gitea
git push github --tags        # taggen till GitHub Releases
```

## Branch-strategi
- `main` – alltid körbar, skyddad
- `feature/beskrivning` – ny funktionalitet
- `fix/beskrivning` – buggfixar
- `chore/beskrivning` – infrastruktur, dokumentation

## Commit-format (conventional commits)
```
feat: lägg till OAuth token refresh
fix: korrigera momsberäkning för MP2
chore: uppdatera beroenden
test: lägg till enhetstester för valideringsmotor
docs: uppdatera CLAUDE.md med nya BAS-konton
```

## Vad Claude aldrig pushar automatiskt
- Direkt till `main` utan att tester passerar
- Till `github` remote utan explicit release-beslut från användaren
- Filer som matchar .gitignore (tokens, .env, loggar)
