# Nova Helper — правила Nova RP поверх игры

Программа для Windows: правила проекта Nova RP в отдельном окне, которое открывается поверх игры по **Alt+Q**. Поиск по всем документам, избранное, копирование пунктов для жалоб.

Программа сама проверяет этот репозиторий (при запуске и раз в 6 часов) и скачивает обновлённые правила и новые версии себя.

## Что где лежит

| Папка / файл | Что это |
|---|---|
| `rules/` | Правила, по одному документу в файле `.txt` |
| `manifest.json` | Список актуальных файлов и их контрольные суммы — по нему программа понимает, что обновилось |
| `app/NovaHelper.exe` | Последняя версия программы |
| `src/` | Исходный код (Go, без сторонних библиотек) |
| `tools/make_manifest.py` | Пересобирает `manifest.json` после изменений |
| `Instrukciya.txt` | Инструкция для пользователя |

## Установщик

`setup/NovaHelper-Setup.exe` — один файл для раздачи игрокам. Ставит программу в `%LOCALAPPDATA%\Programs\Nova Helper` (без прав администратора), создаёт ярлыки, запись в «Приложения и возможности» и `Uninstall.exe`. Повторный запуск обновляет программу, сохраняя настройки и избранное.

Собрать: `sh tools/build_installer.sh` (собирает и программу, и установщик).

Скачать: https://github.com/VIRUS1SHOW/nova-helper/raw/main/setup/NovaHelper-Setup.exe

## Как обновить правила

1. Поменять или добавить файл в `rules/` (формат описан в `Instrukciya.txt`).
2. `python3 tools/make_manifest.py`
3. Закоммитить и отправить в `main` — у всех программа подтянет изменения сама.

## Как выпустить новую версию программы

1. Поднять `appVersion` в `src/updater.go`.
2. Собрать: `cd src && GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-H windowsgui -s -w" -o ../app/NovaHelper.exe .`
3. `python3 tools/make_manifest.py --app-version X.Y.Z --notes "что нового"`
4. Закоммитить и отправить.

Ресурсы exe (иконка и манифест) лежат в `src/rsrc_windows_amd64.syso`, собраны утилитой [rsrc](https://github.com/akavel/rsrc) из `icon.ico` и `app.manifest`.
