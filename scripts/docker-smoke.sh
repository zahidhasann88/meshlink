#!/usr/bin/env bash
set -euo pipefail
export MSYS_NO_PATHCONV=1
image="${IMAGE:-meshlink:smoke}"
net=meshlink-smoke
vol=meshlink-smoke-data

cleanup() {
	docker rm -f smoke-relay smoke-a smoke-b smoke-c >/dev/null 2>&1 || true
	docker network rm "$net" >/dev/null 2>&1 || true
	docker volume rm "$vol" >/dev/null 2>&1 || true
}
trap cleanup EXIT
cleanup

[ -n "${SKIP_BUILD:-}" ] || docker build -q -f deployments/docker/Dockerfile -t "$image" . >/dev/null
docker network create "$net" >/dev/null
docker volume create "$vol" >/dev/null
docker run --rm --user 0 --entrypoint chown -v "$vol:/data" "$image" 10001:10001 /data

ml() { docker run --rm -v "$vol:/data" "$image" "$@"; }
sh_in() { docker run --rm -v "$vol:/data" --entrypoint sh "$image" -c "$1"; }

R=$(ml identity create --key /data/relay.key)
A=$(ml identity create --key /data/a.key)
B=$(ml identity create --key /data/b.key)
C=$(ml identity create --key /data/c.key)
relay="\"relay\":{\"address\":\"smoke-relay:7100\",\"id\":\"$R\"}"
sh_in "echo '{\"key_path\":\"/data/b.key\",\"listen_address\":\"0.0.0.0:7002\",\"allowed_peers\":[\"$A\",\"$C\"],$relay}' > /data/b.json"
sh_in "echo '{\"key_path\":\"/data/a.key\",\"listen_address\":\"0.0.0.0:7001\",\"allowed_peers\":[\"$B\"],\"known_peers\":[{\"id\":\"$B\",\"address\":\"smoke-b:7002\"}]}' > /data/a.json"
sh_in "echo '{\"key_path\":\"/data/c.key\",\"listen_address\":\"0.0.0.0:7003\",\"allowed_peers\":[\"$B\"],$relay,\"known_peers\":[{\"id\":\"$B\"}]}' > /data/c.json"

run_bg() { docker run -d --name "$1" --network "$net" -v "$vol:/data:ro" "${@:2}" >/dev/null; }
run_bg smoke-relay --entrypoint meshlink-relay "$image" --key /data/relay.key --listen 0.0.0.0:7100 --allow "$B,$C"
run_bg smoke-b "$image" node start --config /data/b.json --echo
run_bg smoke-a "$image" node start --config /data/a.json
run_bg smoke-c "$image" node start --config /data/c.json

request() {
	for _ in $(seq 1 30); do
		if out=$(docker exec "$1" meshlink message request "$B" "$2" 2>/dev/null) && [ "$out" = "$2" ]; then
			echo "ok: $1 -> b ($2)"
			return 0
		fi
		sleep 1
	done
	echo "FAILED: $1 -> b" >&2
	docker logs "$1" >&2
	return 1
}
request smoke-a "direct over docker network"
request smoke-c "through the relay"
docker exec smoke-c meshlink peer list | grep -q "via relay" || { echo "FAILED: c is not relayed" >&2; exit 1; }
docker exec smoke-a meshlink peer list | grep -vq "via relay" || { echo "FAILED: a should be direct" >&2; exit 1; }
docker stop -t 10 smoke-a smoke-b smoke-c smoke-relay >/dev/null
for c in smoke-a smoke-b smoke-c smoke-relay; do
	code=$(docker inspect -f '{{.State.ExitCode}}' "$c")
	[ "$code" = 0 ] || { echo "FAILED: $c exited with $code" >&2; docker logs "$c" >&2; exit 1; }
done
echo "docker smoke test passed"
