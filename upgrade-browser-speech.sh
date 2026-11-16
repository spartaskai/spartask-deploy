#!/bin/sh
# Run as root from the installed stack directory after closing all meetings.
set -eu
bundle=${1:?Usage: upgrade-browser-speech.sh BUNDLE_DIRECTORY APP_VERSION}
version=${2:?Application release version required}
test -f .env
test -f spartaskctl
test -f "$bundle/spartaskctl"
test -f "$bundle/browser-speech-preflight.sql"
test -f "$bundle/.env.example"
test "$(id -u)" -eq 0

# The previous configuration still runs here. This query changes no data.
docker compose exec -T db sh -c 'exec psql -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"' < "$bundle/browser-speech-preflight.sql"

backup="$(pwd)/browser-speech-backup-$(date +%Y%m%d%H%M%S)"
mkdir -m 700 "$backup"
for file in spartaskctl .env .env.example compose.meeting.generated.yml speech-profile.json; do
  if test -f "$file"; then cp -p "$file" "$backup/"; fi
done
if test -f .secrets/meeting.caddy; then
  mkdir -m 700 "$backup/.secrets"
  cp -p .secrets/meeting.caddy "$backup/.secrets/"
fi
cat > "$backup/rollback.sh" <<'ROLLBACK'
#!/bin/sh
set -eu
saved=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
stack=$(dirname -- "$saved")
cd "$stack"
for file in spartaskctl .env .env.example compose.meeting.generated.yml speech-profile.json; do
  if test -f "$saved/$file"; then cp -p "$saved/$file" "./$file"; fi
done
if test -f "$saved/.secrets/meeting.caddy"; then
  cp -p "$saved/.secrets/meeting.caddy" .secrets/meeting.caddy
fi
./spartaskctl restart
./spartaskctl status
ROLLBACK
chmod 700 "$backup/rollback.sh"
printf 'Rollback: sudo sh %s/rollback.sh\n' "$backup"

install -m 755 "$bundle/spartaskctl" ./spartaskctl
install -m 644 "$bundle/.env.example" ./.env.example
# Configure before update: update applies config and restarts the stack itself.
./spartaskctl config -speech-browser
./spartaskctl config -check
./spartaskctl update "$version"
./spartaskctl status

# update uses --remove-orphans; assert the retired containers are gone even
# if someone accidentally left an additional overlay/profile enabled.
retired=$(docker compose ps -a --format '{{.Service}}' | sed -n '/^voice$/p; /^speech-stt$/p; /^speech-tts$/p')
if test -n "$retired"; then
  printf 'Retired speech containers still exist: %s\n' "$retired" >&2
  exit 1
fi
printf 'Browser speech configured. Reload the web app; test microphone transcription and agent playback.\n'
