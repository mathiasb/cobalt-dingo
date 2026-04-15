#!/usr/bin/env bash
set -euo pipefail

echo "Project name:"
read name
echo "Client tag (or 'personal'):"
read client

sed -i '' "s/{{PROJECT_NAME}}/$name/g" .context/PROJECT.md 2>/dev/null || sed -i "s/{{PROJECT_NAME}}/$name/g" .context/PROJECT.md
sed -i '' "s/{{CLIENT_TAG or \"personal\"}}/$client/g" .context/PROJECT.md 2>/dev/null || sed -i "s/{{CLIENT_TAG or \"personal\"}}/$client/g" .context/PROJECT.md
sed -i '' "s/{{CLIENT_TAG}}/$client/g" .context/PROJECT.md 2>/dev/null || sed -i "s/{{CLIENT_TAG}}/$client/g" .context/PROJECT.md

task context:sync
echo "✓ Project initialized: $name (client: $client)"
