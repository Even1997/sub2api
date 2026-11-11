#!/usr/bin/env bash
set -Eeuo pipefail
VERSION=2.20.0
RELEASE=v2.20.0-20261111
IMAGE="sub2api:$RELEASE"
WORKER_IMAGE="sub2api-openai-reauth-worker:$RELEASE"
SOURCE="/tmp/sub2api-$RELEASE"
BACKUP_DIR="/var/backups/sub2api/$RELEASE"
BACKUP="/etc/caddy/Caddyfile.bak-$RELEASE"
OLD=(sub2api-direct40-20261111 sub2api-mr-direct40-20261111)
NEW=("sub2api-$RELEASE" "sub2api-mr-$RELEASE")
PORTS=(18280 18281)
OLD_PORTS=(18278 18279)
REFS=(3 2)
DBS=(sub2api sub2api_mr)
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
sql() { sudo -n docker exec sub2api-postgres psql -v ON_ERROR_STOP=1 -U sub2api -d "$1" -Atc "$2"; }
idle() {
  for db in "${DBS[@]}"; do
    test "$(sql "$db" "SELECT count(*) FROM openai_oauth_reauth_tasks WHERE status IN ('queued','running','callback_processing')")" = 0
  done
}

build() {
  local archive_sha="$1" commit="$2"
  printf '%s  %s\n' "$archive_sha" "/tmp/$RELEASE.tar.gz" | sha256sum -c -
  test ! -e "$SOURCE"
  mkdir "$SOURCE"
  tar -xzf "/tmp/$RELEASE.tar.gz" -C "$SOURCE"
  cd "$SOURCE"
  test "$(tr -d '\r\n' < backend/cmd/server/VERSION)" = "$VERSION"
  test ! -e backend/config/config.yaml
  test ! -d backend/internal/service/data
  sudo -n docker build --build-arg VERSION="$VERSION" --build-arg COMMIT="$commit" \
    --build-arg DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    --label "org.opencontainers.image.revision=$commit" --label "org.opencontainers.image.version=$VERSION" \
    -t "$IMAGE" -f deploy/Dockerfile .
  sudo -n docker build --build-context turb=/home/ubuntu/src/reauth-contexts/turb \
    --build-context tosub2=/home/ubuntu/src/reauth-contexts/tosub2 \
    --label "org.opencontainers.image.revision=$commit" \
    -f tools/Dockerfile.openai-oauth-reauth-worker -t "$WORKER_IMAGE" .
  sudo -n docker run --rm --network none --entrypoint python \
    -v "$SOURCE/tools:/validation/tools:ro" -w /validation "$WORKER_IMAGE" \
    -m unittest tools.test_openai_oauth_reauth_worker_pool tools.test_openai_oauth_reauth_runtime_controls \
    tools.test_openai_oauth_reauth_worker tools.test_openai_oauth_reauth_engines tools.test_reauth_node_resident -q
  sudo -n docker run --rm --network none "$IMAGE" --version 2>&1 | grep -F "Sub2API $VERSION"
}

backup() {
  sudo -n test ! -e "$BACKUP_DIR"
  sudo -n install -d -m 700 "$BACKUP_DIR"
  sudo -n cp /etc/caddy/Caddyfile "$BACKUP_DIR/Caddyfile"
  for db in "${DBS[@]}"; do
    sudo -n docker exec sub2api-postgres pg_dump -U sub2api -Fc -Z1 "$db" | sudo -n tee "$BACKUP_DIR/$db.dump" >/dev/null
    sudo -n cat "$BACKUP_DIR/$db.dump" | sudo -n docker exec -i sub2api-postgres pg_restore --list >/dev/null
    sudo -n chmod 600 "$BACKUP_DIR/$db.dump"
    printf 'database_backup_verified %s\n' "$db"
  done
  for worker in "${WORKERS[@]}"; do
    sudo -n docker cp -a "$worker:/home/worker/totp-recovery" "$BACKUP_DIR/$worker-recovery"
  done
  sudo -n touch "$BACKUP_DIR/complete"
}

prepare() {
  local i target_id ready bind
  sudo -n test -f "$BACKUP_DIR/complete"
  sudo -n docker image inspect "$IMAGE" "$WORKER_IMAGE" >/dev/null
  idle
  for i in 0 1; do
    test "$(sudo -n docker inspect -f '{{.State.Health.Status}}' "${OLD[$i]}")" = healthy
    test "$(sudo -n grep -c "127.0.0.1:${OLD_PORTS[$i]}" /etc/caddy/Caddyfile)" = "${REFS[$i]}"
    test "$(sql "${DBS[$i]}" "SELECT count(*) FROM accounts WHERE deleted_at IS NULL AND extra ? 'oauth_degradation_action'")" = 0
    if sudo -n ss -ltn | grep -qE ":${PORTS[$i]}\b"; then echo 'Target port occupied' >&2; return 1; fi
    if sudo -n docker inspect "${NEW[$i]}" >/dev/null 2>&1; then echo 'Target container exists' >&2; return 1; fi
    if sudo -n docker inspect "${WORKERS[$i]}-bak-$RELEASE" >/dev/null 2>&1; then return 1; fi
    test "$(sudo -n docker inspect -f '{{len .Mounts}}' "${WORKERS[$i]}")" = 0
    target_id=$(sudo -n docker inspect -f '{{.Id}}' "${OLD[$i]}")
    test "$(sudo -n docker inspect -f '{{.HostConfig.NetworkMode}}' "${WORKERS[$i]}")" = "container:$target_id"
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
    target_id=$(sudo -n docker inspect -f '{{.Id}}' "${NEW[$i]}")
    sudo -n docker run --rm --network none --env-file "$env_file" "$WORKER_IMAGE" --check
    sudo -n docker create --name "${WORKERS[$i]}-green-$RELEASE" --restart unless-stopped \
      --network "container:$target_id" --env-file "$env_file" "$WORKER_IMAGE"
    rm -f "$env_file"; env_file=''
    printf 'green_prepared %s port=%s\n' "${NEW[$i]}" "${PORTS[$i]}"
  done
}

verify() {
  local i id mode count
  for i in 0 1; do
    test "$(sudo -n docker inspect -f '{{.State.Health.Status}}' "${NEW[$i]}")" = healthy
    test "$(sudo -n docker inspect -f '{{.Config.Image}}' "${NEW[$i]}")" = "$IMAGE"
    health "http://127.0.0.1:${PORTS[$i]}/health"
    sudo -n docker exec "${NEW[$i]}" /app/sub2api --version 2>&1 | grep -F "Sub2API $VERSION"
    test "$(sudo -n grep -c "127.0.0.1:${PORTS[$i]}" /etc/caddy/Caddyfile)" = "${REFS[$i]}"
    ! sudo -n grep -q "127.0.0.1:${OLD_PORTS[$i]}" /etc/caddy/Caddyfile
    count=$(sql "${DBS[$i]}" "SELECT count(*) FROM schema_migrations WHERE filename IN ('344_oauth_degradation_native_controls.sql','345_quality_oauth_401_streaks.sql','346_quality_oauth_401_trusted_revocation.sql')")
    test "$count" = 3
    sudo -n docker exec "${NEW[$i]}" sh -c 'test -z "$CLOUDCAT_PROXY_URL" && test -z "$CLOUDCAT_API_PROXY_URL" && test -z "$HTTP_PROXY" && test -z "$HTTPS_PROXY" && test -z "$ALL_PROXY"'
    id=$(sudo -n docker inspect -f '{{.Id}}' "${NEW[$i]}")
    mode=$(sudo -n docker inspect -f '{{.HostConfig.NetworkMode}}' "${WORKERS[$i]}")
    test "$mode" = "container:$id"
    test "$(sudo -n docker inspect -f '{{.Config.Image}}' "${WORKERS[$i]}")" = "$WORKER_IMAGE"
    test "$(sudo -n docker inspect -f '{{.State.Running}}' "${WORKERS[$i]}")" = true
    test "$(sudo -n docker inspect -f '{{.RestartCount}}' "${WORKERS[$i]}")" = 0
    sudo -n docker logs "${WORKERS[$i]}" 2>&1 | grep -F ' ready' >/dev/null
    sudo -n docker exec "${WORKERS[$i]}" python -c 'import openai_oauth_reauth_worker as w; print("authenticated_runtime_concurrency", w.WorkerAPI(w.WorkerConfig.from_env()).runtime_concurrency())'
    printf 'verified app=%s worker=%s target=%s migrations=%s\n' "${NEW[$i]}" "${WORKERS[$i]}" "$id" "$count"
  done
  public_health
}

rollback() {
  set +e
  sudo -n docker start "${OLD[@]}" >/dev/null
  if sudo -n test -f "$BACKUP"; then
    sudo -n cp "$BACKUP" /etc/caddy/Caddyfile
    sudo -n caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile && sudo -n systemctl reload caddy
  fi
  for i in 0 1; do
    local archived="${WORKERS[$i]}-bak-$RELEASE"
    if sudo -n docker inspect "$archived" >/dev/null 2>&1; then
      if sudo -n docker inspect "${WORKERS[$i]}" >/dev/null 2>&1; then
        sudo -n docker stop --time 60 "${WORKERS[$i]}" >/dev/null
        # Preserve any recovery records created since cutover when returning to blue.
        sudo -n docker cp -a "${WORKERS[$i]}:/home/worker/totp-recovery" - | sudo -n docker cp -a - "$archived:/home/worker/"
        sudo -n docker rename "${WORKERS[$i]}" "${WORKERS[$i]}-failed-$RELEASE"
      fi
      sudo -n docker rename "$archived" "${WORKERS[$i]}"
    fi
    sudo -n docker start "${WORKERS[$i]}" >/dev/null
  done
  sudo -n docker stop --time 60 "${NEW[@]}" >/dev/null
  public_health
  echo 'ROLLED_BACK: application routing only; database backups retained without overwriting newer orders'
}

cutover() {
  local i ready
  idle
  sudo -n test ! -e "$BACKUP"
  for i in 0 1; do
    test "$(sudo -n docker inspect -f '{{.State.Health.Status}}' "${NEW[$i]}")" = healthy
    health "http://127.0.0.1:${PORTS[$i]}/health"
    test "$(sudo -n grep -c "127.0.0.1:${OLD_PORTS[$i]}" /etc/caddy/Caddyfile)" = "${REFS[$i]}"
    test "$(sudo -n docker inspect -f '{{.State.Status}}' "${WORKERS[$i]}-green-$RELEASE")" = created
  done
  sudo -n cp /etc/caddy/Caddyfile "$BACKUP"
  trap 'trap - ERR; rollback; exit 1' ERR
  sudo -n sed -i 's#127.0.0.1:18278#127.0.0.1:18280#g; s#127.0.0.1:18279#127.0.0.1:18281#g' /etc/caddy/Caddyfile
  sudo -n caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile >/dev/null 2>&1
  sudo -n systemctl reload caddy
  public_health
  for i in 0 1; do
    sudo -n docker stop --time 90 "${WORKERS[$i]}" >/dev/null
    sudo -n docker rename "${WORKERS[$i]}" "${WORKERS[$i]}-bak-$RELEASE"
    sudo -n docker rename "${WORKERS[$i]}-green-$RELEASE" "${WORKERS[$i]}"
    sudo -n docker cp -a "${WORKERS[$i]}-bak-$RELEASE:/home/worker/totp-recovery" - | sudo -n docker cp -a - "${WORKERS[$i]}:/home/worker/"
    sudo -n docker start "${WORKERS[$i]}" >/dev/null
    ready=0
    for _ in $(seq 1 45); do
      if sudo -n docker logs "${WORKERS[$i]}" 2>&1 | grep -F ' ready' >/dev/null; then ready=1; break; fi
      sleep 2
    done
    test "$ready" = 1
  done
  verify
  sudo -n docker stop --time 90 "${OLD[@]}" >/dev/null
  trap - ERR
  echo "DEPLOYED $IMAGE backup=$BACKUP_DIR"
}

case "${1:-}" in
  build) build "${2:?archive SHA required}" "${3:?commit required}" ;;
  backup) backup ;;
  prepare) prepare ;;
  cutover) cutover ;;
  verify) verify ;;
  rollback) rollback ;;
  *) echo 'Expected build, backup, prepare, cutover, verify, or rollback' >&2; exit 2 ;;
esac
