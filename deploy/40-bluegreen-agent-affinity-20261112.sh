#!/usr/bin/env bash
set -Eeuo pipefail

RELEASE=v2.20.0-agent-affinity-20261112
VERSION=2.20.0+agent-affinity.20261112
BASE=abd5dd740f2a3b71c536d4940fe476b63dc93fb3
IMAGE="sub2api:$RELEASE"
SOURCE="/tmp/sub2api-$RELEASE"
BACKUP="/etc/caddy/Caddyfile.bak-$RELEASE"
BACKUP_DIR="/var/backups/sub2api/$RELEASE"
OLD=(sub2api-v2.20.0-20261111 sub2api-mr-v2.20.0-20261111)
NEW=("sub2api-$RELEASE" "sub2api-mr-$RELEASE")
PORTS=(18282 18283)
OLD_PORTS=(18280 18281)
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
build() {
  cd /tmp
  sha256sum -c "$RELEASE.sha256"
  test ! -e "$SOURCE"
  mkdir "$SOURCE"
  tar -xf "$RELEASE-base.tar" -C "$SOURCE"
  cd "$SOURCE"
  git apply --check "/tmp/$RELEASE.patch"
  git apply "/tmp/$RELEASE.patch"
  tar -xf "/tmp/$RELEASE-tests.tar" -C "$SOURCE"
  test "$(tr -d '\r\n' < backend/cmd/server/VERSION)" = 2.20.0
  test ! -e backend/config/config.yaml
  test ! -d backend/internal/service/data
  local -a build_args=(--build-arg "VERSION=$VERSION" --build-arg "COMMIT=$BASE-agent-affinity"
    --build-arg "DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ)")
  sudo -n docker build "${build_args[@]}" --target backend-builder -t "$IMAGE-builder" -f deploy/Dockerfile .
  sudo -n docker run --rm --network none -e GOMAXPROCS=4 -v "$SOURCE/backend:/validation/backend:ro" \
    -w /validation/backend --entrypoint go "$IMAGE-builder" \
    test ./internal/service -run '^Test(Issue715|Issue758|WithOpenAIGuardianParentAffinity|OpenAIGatewayService_Guardian)' -count=1 -timeout=3m
  sudo -n docker build "${build_args[@]}" \
    --label "org.opencontainers.image.revision=$BASE" --label "org.opencontainers.image.version=$VERSION" --label "io.sub2api.release=$RELEASE" \
    --label "io.sub2api.patch-sha256=$(sha256sum "/tmp/$RELEASE.patch" | cut -d' ' -f1)" \
    --label io.sub2api.source-scope=live-baseline-plus-agent-affinity \
    -t "$IMAGE" -f deploy/Dockerfile .
  sudo -n docker run --rm --network none "$IMAGE" --version 2>&1 | grep -F "Sub2API $VERSION"
}
prepare() {
  local i id ready bind worker_image
  sudo -n docker image inspect "$IMAGE" >/dev/null
  sudo -n test ! -e "$BACKUP_DIR"
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
  sudo -n install -d -m 700 "$BACKUP_DIR"
  sudo -n cp /etc/caddy/Caddyfile "$BACKUP_DIR/Caddyfile"
  sudo -n cp "${BASH_SOURCE[0]}" "$BACKUP_DIR/deploy.sh"
  sudo -n cp "/tmp/$RELEASE.patch" "/tmp/$RELEASE.sha256" "$BACKUP_DIR/"
  sudo -n chmod 700 "$BACKUP_DIR/deploy.sh"
  umask 077
  for i in 0 1; do
    sudo -n docker cp -a "${WORKERS[$i]}:/home/worker/totp-recovery" "$BACKUP_DIR/${WORKERS[$i]}-recovery"
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
    printf 'verified app=%s port=%s worker=%s target=%s\n' "${NEW[$i]}" "${PORTS[$i]}" "${WORKERS[$i]}" "$id"
  done
  public_health
  public_version
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
  sudo -n test ! -e "$BACKUP"
  for i in 0 1; do
    test "$(sudo -n docker inspect -f '{{.State.Health.Status}}' "${NEW[$i]}")" = healthy
    health "http://127.0.0.1:${PORTS[$i]}/health"
    test "$(sudo -n grep -c "127.0.0.1:${OLD_PORTS[$i]}" /etc/caddy/Caddyfile)" = "${REFS[$i]}"
    test "$(sudo -n docker inspect -f '{{.State.Status}}' "${WORKERS[$i]}-green-$RELEASE")" = created
  done
  sudo -n cp /etc/caddy/Caddyfile "$BACKUP"
  trap 'trap - ERR; rollback; exit 1' ERR
  sudo -n sed -i 's#127.0.0.1:18280#127.0.0.1:18282#g; s#127.0.0.1:18281#127.0.0.1:18283#g' /etc/caddy/Caddyfile
  sudo -n caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile >/dev/null 2>&1
  sudo -n systemctl reload caddy
  public_health
  for i in 0 1; do
    sudo -n docker stop --time 60 "${WORKERS[$i]}" >/dev/null
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
  build) build ;;
  prepare) prepare ;;
  cutover) cutover ;;
  verify) verify ;;
  retire) retire ;;
  rollback) rollback ;;
  *) echo 'Expected build, prepare, cutover, verify, retire, or rollback' >&2; exit 2 ;;
esac
