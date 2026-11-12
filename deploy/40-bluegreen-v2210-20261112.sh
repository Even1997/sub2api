#!/usr/bin/env bash
set -Eeuo pipefail

RELEASE=v2.21.0-20261112
VERSION=2.21.0
BASE=0d001f84fc4096b01c193b71b36f3f20c407c84a
IMAGE=ghcr.io/even1997/sub2api:2.21.0
BACKUP="/etc/caddy/Caddyfile.bak-$RELEASE"
BACKUP_DIR="/var/backups/sub2api/$RELEASE"
OLD=(sub2api-v2.20.0-agent-affinity-20261112 sub2api-mr-v2.20.0-agent-affinity-20261112)
NEW=("sub2api-$RELEASE" "sub2api-mr-$RELEASE")
PORTS=(18284 18285)
OLD_PORTS=(18282 18283)
REFS=(3 2)
WORKERS=(sub2api-openai-reauth-worker sub2api-openai-reauth-worker-mr)
NET=sub2api-deploy_sub2api-network
env_file=''
trap '[ -z "$env_file" ] || rm -f "$env_file"' EXIT

health() { curl -fsS --max-time 10 "$1" | grep -q '"status":"ok"'; }
public_health() {
  for domain in xymm.win ai.xymm.win mr.xymm.win dogmaster.lol; do
    health "https://$domain/health"
    printf 'public_health %s ok\n' "$domain"
  done
}
public_version() {
  for domain in xymm.win ai.xymm.win mr.xymm.win dogmaster.lol; do
    curl -fsS --max-time 15 "https://$domain/api/v1/settings/public?deployment=$RELEASE" |
      python3 -c 'import json,sys; actual=json.load(sys.stdin)["data"]["version"]; assert actual == sys.argv[1], actual; print("public_version", sys.argv[2], actual)' "$VERSION" "$domain"
  done
}
idle() {
  for db in sub2api sub2api_mr; do
    count=$(sudo -n docker exec sub2api-postgres psql -X -U sub2api -d "$db" -Atc \
      "SELECT count(*) FROM openai_oauth_reauth_tasks WHERE status IN ('queued','running','callback_processing')")
    test "$count" = 0
  done
}
backup() {
  sudo -n test ! -e "$BACKUP_DIR"
  sudo -n install -d -m 700 "$BACKUP_DIR"
  sudo -n cp /etc/caddy/Caddyfile "$BACKUP_DIR/Caddyfile"
  sudo -n cp "${BASH_SOURCE[0]}" "$BACKUP_DIR/deploy.sh"
  sudo -n chmod 700 "$BACKUP_DIR/deploy.sh"
  for db in sub2api sub2api_mr; do
    sudo -n docker exec sub2api-postgres pg_dump -U sub2api -d "$db" -Fc -Z 1 |
      sudo -n tee "$BACKUP_DIR/$db.dump" >/dev/null
    sudo -n docker run --rm --network none -v "$BACKUP_DIR:/backup:ro" postgres:18-alpine \
      pg_restore --file=/dev/null "/backup/$db.dump"
    sudo -n chmod 600 "$BACKUP_DIR/$db.dump"
    sudo -n sha256sum "$BACKUP_DIR/$db.dump"
    printf 'database_backup %s verified\n' "$db"
  done
  for i in 0 1; do
    sudo -n docker inspect "${OLD[$i]}" "${WORKERS[$i]}" |
      sudo -n tee "$BACKUP_DIR/inspect-$i.json" >/dev/null
    sudo -n chmod 600 "$BACKUP_DIR/inspect-$i.json"
    sudo -n docker cp -a "${WORKERS[$i]}:/home/worker/totp-recovery" "$BACKUP_DIR/${WORKERS[$i]}-recovery"
  done
  sudo -n touch "$BACKUP_DIR/BACKUP_COMPLETE"
  echo BACKUP_COMPLETE
}
build() {
  sudo -n docker pull "$IMAGE"
  test "$(sudo -n docker image inspect -f '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$IMAGE")" = "$BASE"
  sudo -n docker run --rm --network none "$IMAGE" --version 2>&1 | grep -F "Sub2API $VERSION"
  sudo -n docker image inspect -f '{{.Id}} {{json .RepoDigests}}' "$IMAGE"
}

prepare() {
  local i id ready bind worker_image
  sudo -n docker image inspect "$IMAGE" >/dev/null
  sudo -n test -f "$BACKUP_DIR/BACKUP_COMPLETE"
  for i in 0 1; do
    test "$(sudo -n docker inspect -f '{{.State.Health.Status}}' "${OLD[$i]}")" = healthy
    test "$(sudo -n grep -c "127.0.0.1:${OLD_PORTS[$i]}" /etc/caddy/Caddyfile)" = "${REFS[$i]}"
    if sudo -n ss -ltn | grep -qE ":${PORTS[$i]}\b"; then return 1; fi
    if sudo -n docker inspect "${NEW[$i]}" >/dev/null 2>&1; then return 1; fi
    if sudo -n docker inspect "${WORKERS[$i]}-bak-$RELEASE" >/dev/null 2>&1; then return 1; fi
    test "$(sudo -n docker inspect -f '{{len .Mounts}}' "${WORKERS[$i]}")" = 0
    test "$(sudo -n docker inspect -f '{{.HostConfig.NetworkMode}}' "${OLD[$i]}")" = "$NET"
    id=$(sudo -n docker inspect -f '{{.Id}}' "${OLD[$i]}")
    test "$(sudo -n docker inspect -f '{{.HostConfig.NetworkMode}}' "${WORKERS[$i]}")" = "container:$id"
  done
  umask 077
  for i in 0 1; do
    env_file=$(mktemp "/tmp/$RELEASE-app.XXXXXX")
    sudo -n docker inspect -f '{{range .Config.Env}}{{println .}}{{end}}' "${OLD[$i]}" > "$env_file"
    local -a binds=()
    while IFS= read -r bind; do [ -z "$bind" ] || binds+=(-v "$bind"); done < <(sudo -n docker inspect -f '{{range .HostConfig.Binds}}{{println .}}{{end}}' "${OLD[$i]}")
    sudo -n docker run -d --name "${NEW[$i]}" --restart unless-stopped --network "$NET" \
      --env-file "$env_file" -p "127.0.0.1:${PORTS[$i]}:8080" "${binds[@]}" "$IMAGE"
    rm -f "$env_file"; env_file=''
    ready=0
    for _ in $(seq 1 120); do
      if test "$(sudo -n docker inspect -f '{{.State.Health.Status}}' "${NEW[$i]}")" = healthy && health "http://127.0.0.1:${PORTS[$i]}/health"; then ready=1; break; fi
      sleep 2
    done
    test "$ready" = 1
    env_file=$(mktemp "/tmp/$RELEASE-worker.XXXXXX")
    sudo -n docker inspect -f '{{range .Config.Env}}{{println .}}{{end}}' "${WORKERS[$i]}" > "$env_file"
    worker_image=$(sudo -n docker inspect -f '{{.Image}}' "${WORKERS[$i]}")
    id=$(sudo -n docker inspect -f '{{.Id}}' "${NEW[$i]}")
    sudo -n docker run --rm --network none --env-file "$env_file" "$worker_image" --check
    sudo -n docker create --name "${WORKERS[$i]}-green-$RELEASE" --restart unless-stopped \
      --network "container:$id" --env-file "$env_file" "$worker_image"
    rm -f "$env_file"; env_file=''
    printf 'green_prepared %s port=%s\n' "${NEW[$i]}" "${PORTS[$i]}"
  done
}
verify() {
  local i id
  for i in 0 1; do
    test "$(sudo -n docker inspect -f '{{.State.Health.Status}}' "${NEW[$i]}")" = healthy
    test "$(sudo -n docker inspect -f '{{.Config.Image}}' "${NEW[$i]}")" = "$IMAGE"
    test "$(sudo -n docker inspect -f '{{.RestartCount}}' "${NEW[$i]}")" = 0
    health "http://127.0.0.1:${PORTS[$i]}/health"
    sudo -n docker exec "${NEW[$i]}" /app/sub2api --version 2>&1 | grep -F "Sub2API $VERSION"
    test "$(sudo -n grep -c "127.0.0.1:${PORTS[$i]}" /etc/caddy/Caddyfile)" = "${REFS[$i]}"
    ! sudo -n grep -q "127.0.0.1:${OLD_PORTS[$i]}" /etc/caddy/Caddyfile
    id=$(sudo -n docker inspect -f '{{.Id}}' "${NEW[$i]}")
    test "$(sudo -n docker inspect -f '{{.HostConfig.NetworkMode}}' "${WORKERS[$i]}")" = "container:$id"
    test "$(sudo -n docker inspect -f '{{.Image}}' "${WORKERS[$i]}")" = "$(sudo -n docker inspect -f '{{.Image}}' "${WORKERS[$i]}-bak-$RELEASE")"
    test "$(sudo -n docker inspect -f '{{.State.Running}}' "${WORKERS[$i]}")" = true
    test "$(sudo -n docker inspect -f '{{.RestartCount}}' "${WORKERS[$i]}")" = 0
    sudo -n docker logs "${WORKERS[$i]}" 2>&1 | grep -F ' ready' >/dev/null
    sudo -n docker exec "${WORKERS[$i]}" python -c 'import openai_oauth_reauth_worker as w; print("authenticated_runtime_concurrency", w.WorkerAPI(w.WorkerConfig.from_env()).runtime_concurrency())'
    sudo -n docker exec "${WORKERS[$i]}" python -c 'import json,urllib.request,openai_oauth_reauth_worker as w; c=w.WorkerConfig.from_env(); v=json.load(urllib.request.urlopen(c.base_url+"/api/v1/settings/public",timeout=15))["data"]["version"]; assert v=="2.21.0",v; print("worker_origin_version",v)'
    sudo -n docker inspect "${OLD[$i]}" "${NEW[$i]}" "${WORKERS[$i]}-bak-$RELEASE" "${WORKERS[$i]}" | python3 -c 'import json,sys; a,b,c,d=json.load(sys.stdin); env=lambda x:dict(s.split("=",1) for s in x["Config"]["Env"]); assert env(a).items()<=env(b).items(); assert env(c)==env(d); assert a["Mounts"]==b["Mounts"]; assert a["HostConfig"]["RestartPolicy"]==b["HostConfig"]["RestartPolicy"]; assert c["HostConfig"]["RestartPolicy"]==d["HostConfig"]["RestartPolicy"]; print("environment_mounts_restart_preserved")'
    printf 'verified app=%s port=%s worker=%s target=%s\n' "${NEW[$i]}" "${PORTS[$i]}" "${WORKERS[$i]}" "$id"
  done
  public_health
  public_version
  for domain in xymm.win ai.xymm.win mr.xymm.win dogmaster.lol; do
    test "$(curl -sS --max-time 15 -o /dev/null -w '%{http_code}' -X POST -H 'Content-Type: application/json' -d '{}' "https://$domain/v1/responses")" = 401
    printf 'public_unauthenticated_responses %s 401\n' "$domain"
  done
  python3 - <<'PY'
from html.parser import HTMLParser
from urllib.parse import urljoin
import subprocess
def read(url):
    return subprocess.check_output(["curl", "-fsS", "--max-time", "15", url])
class Assets(HTMLParser):
    def __init__(self):
        super().__init__()
        self.urls = []
    def handle_starttag(self, tag, attrs):
        attrs = dict(attrs)
        if tag == "script" and attrs.get("type") == "module" and attrs.get("src"):
            self.urls.append(attrs["src"])
        if tag == "link" and attrs.get("rel") == "stylesheet":
            self.urls.append(attrs["href"])
for domain in ("xymm.win", "mr.xymm.win"):
    origin = "https://" + domain
    parser = Assets()
    parser.feed(read(origin).decode())
    assert parser.urls, domain
    for asset in parser.urls:
        data = read(urljoin(origin, asset))
        assert len(data) > 100 and not data.lstrip().lower().startswith(b"<!doctype html"), asset
    print("frontend_assets", domain, len(parser.urls), "ok")
PY
}
rollback() {
  local i archived ready container pid count failed=0
  set +e
  echo ROLLBACK_STARTED
  sudo -n docker start "${OLD[@]}" >/dev/null || failed=1
  for i in 0 1; do
    ready=0
    for _ in $(seq 1 45); do
      if test "$(sudo -n docker inspect -f '{{.State.Health.Status}}' "${OLD[$i]}")" = healthy && health "http://127.0.0.1:${OLD_PORTS[$i]}/health"; then ready=1; break; fi
      sleep 2
    done
    if test "$ready" != 1; then
      echo 'ROLLBACK_BLOCKED: blue is not ready; keep the current routing'
      return 1
    fi
  done
  if sudo -n test -f "$BACKUP"; then
    sudo -n cp "$BACKUP" /etc/caddy/Caddyfile || failed=1
    sudo -n caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile >/dev/null 2>&1 && sudo -n systemctl reload caddy || failed=1
  fi
  for i in 0 1; do
    archived="${WORKERS[$i]}-bak-$RELEASE"
    if sudo -n docker inspect "$archived" >/dev/null 2>&1; then
      if sudo -n docker inspect "${WORKERS[$i]}" >/dev/null 2>&1; then
        sudo -n docker stop --time 60 "${WORKERS[$i]}" >/dev/null || failed=1
        sudo -n docker cp -a "${WORKERS[$i]}:/home/worker/totp-recovery" - | sudo -n docker cp -a - "$archived:/home/worker/" || failed=1
        sudo -n docker rename "${WORKERS[$i]}" "${WORKERS[$i]}-failed-$RELEASE" || failed=1
      fi
      sudo -n docker rename "$archived" "${WORKERS[$i]}" || failed=1
    fi
    sudo -n docker start "${WORKERS[$i]}" >/dev/null || failed=1
  done
  public_health || failed=1
  if test "$failed" = 0; then
    for container in "${NEW[@]}"; do
      pid=$(sudo -n docker inspect -f '{{.State.Pid}}' "$container")
      count=$(sudo -n nsenter -t "$pid" -n ss -Hnt state established '( sport = :8080 )' | wc -l)
      if test "$count" = 0; then
        sudo -n docker stop --time 60 "$container" >/dev/null || failed=1
      else
        printf 'rollback_draining %s connections=%s\n' "$container" "$count"
      fi
    done
  fi
  printf 'ROLLBACK_FINISHED errors=%s\n' "$failed"
  return "$failed"
}
cutover() {
  local i ready
  idle
  for i in 0 1; do
    test "$(sudo -n docker inspect -f '{{.State.Health.Status}}' "${NEW[$i]}")" = healthy
    health "http://127.0.0.1:${PORTS[$i]}/health"
    test "$(sudo -n grep -c "127.0.0.1:${OLD_PORTS[$i]}" /etc/caddy/Caddyfile)" = "${REFS[$i]}"
    test "$(sudo -n docker inspect -f '{{.State.Status}}' "${WORKERS[$i]}-green-$RELEASE")" = created
  done
  if sudo -n test -e "$BACKUP"; then
    sudo -n cmp --silent "$BACKUP" /etc/caddy/Caddyfile
  else
    sudo -n cp /etc/caddy/Caddyfile "$BACKUP"
  fi
  trap 'trap - ERR; rollback; exit 1' ERR
  sudo -n sed -i 's#127.0.0.1:18282#127.0.0.1:18284#g; s#127.0.0.1:18283#127.0.0.1:18285#g' /etc/caddy/Caddyfile
  sudo -n caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile >/dev/null 2>&1
  sudo -n systemctl reload caddy
  public_health
  for i in 0 1; do
    sudo -n docker stop --time 60 "${WORKERS[$i]}" >/dev/null
    sudo -n docker rename "${WORKERS[$i]}" "${WORKERS[$i]}-bak-$RELEASE"
    sudo -n docker rename "${WORKERS[$i]}-green-$RELEASE" "${WORKERS[$i]}"
    sudo -n docker cp -a "${WORKERS[$i]}-bak-$RELEASE:/home/worker/totp-recovery" - | sudo -n docker cp -a - "${WORKERS[$i]}:/home/worker/"
    python3 - "${WORKERS[$i]}-bak-$RELEASE" "${WORKERS[$i]}" <<'PY'
import hashlib,io,subprocess,sys,tarfile
def recovery(container):
    data = subprocess.check_output(["sudo", "-n", "docker", "cp", container+":/home/worker/totp-recovery", "-"])
    with tarfile.open(fileobj=io.BytesIO(data)) as archive:
        return {m.name: hashlib.sha256(archive.extractfile(m).read()).hexdigest() for m in archive if m.isfile()}
before, after = (recovery(container) for container in sys.argv[1:])
assert before == after, "worker recovery records differ"
print("worker_recovery_preserved", len(before))
PY
    sudo -n docker start "${WORKERS[$i]}" >/dev/null
    ready=0
    for _ in $(seq 1 45); do
      if sudo -n docker logs "${WORKERS[$i]}" 2>&1 | grep -F ' ready' >/dev/null; then ready=1; break; fi
      sleep 2
    done
    test "$ready" = 1
  done
  verify
  echo CUTOVER_VERIFIED
  trap - ERR
}
retire() {
  verify
  local container pid count
  for container in "${OLD[@]}"; do
    pid=$(sudo -n docker inspect -f '{{.State.Pid}}' "$container")
    count=$(sudo -n nsenter -t "$pid" -n ss -Hnt state established '( sport = :8080 )' | wc -l)
    printf 'blue_inbound_connections %s %s\n' "$container" "$count"
    if test "$count" != 0; then
      echo 'DRAINING: keep blue running until existing connections finish'
      return 2
    fi
  done
  sudo -n docker stop --time 90 "${OLD[@]}" >/dev/null
  printf 'DEPLOYED image=%s rollback=%s\n' "$IMAGE" "$BACKUP"
}
case "${1:-}" in
  backup) backup ;;
  build) build ;;
  prepare) prepare ;;
  cutover) cutover ;;
  verify) verify ;;
  retire) retire ;;
  rollback) rollback ;;
  *) echo 'Expected backup, build, prepare, cutover, verify, retire, or rollback' >&2; exit 2 ;;
esac
