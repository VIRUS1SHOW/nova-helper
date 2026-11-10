#!/bin/sh
# Собирает программу и установщик setup/NovaHelper-Setup.exe.
# Запуск из корня репозитория: sh tools/build_installer.sh
set -e
ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT/src"

GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-H windowsgui -s -w" -o ../app/NovaHelper.exe .

# payload.zip: программа, инструкция и правила — то, что установщик распакует.
TMP=$(mktemp -d)
mkdir -p "$TMP/rules"
cp ../app/NovaHelper.exe ../Instrukciya.txt "$TMP/"
cp ../rules/*.txt "$TMP/rules/"
rm -f installer/payload.zip
(cd "$TMP" && zip -q -9 -r "$ROOT/src/installer/payload.zip" .)
rm -rf "$TMP"

# Иконка и манифест (asInvoker — без запроса прав администратора) берутся у программы.
cp rsrc_windows_amd64.syso installer/rsrc_windows_amd64.syso

mkdir -p ../setup
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-H windowsgui -s -w" -o ../setup/NovaHelper-Setup.exe ./installer
ls -la ../setup/NovaHelper-Setup.exe
