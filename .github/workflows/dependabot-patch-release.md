---
on:
  workflow_dispatch:
  schedule: weekly on monday

permissions:
  contents: read
  copilot-requests: write
  issues: read
  pull-requests: read

tools:
  github:
    toolsets: [repos, pull_requests]

network:
  allowed:
    - defaults
    - github

timeout-minutes: 45

safe-outputs:
  jobs:
    create-patch-release:
      description: >
        Merge the listed Dependabot pull requests, run the Go tests and the
        integration test, publish a patch release only if every test passes,
        and update the Flux cronjob image to that release.
      runs-on: ubuntu-latest
      output: Patch release published and Flux updated.
      permissions:
        actions: read
        contents: write
        pull-requests: write
      env:
        GH_TOKEN: ${{ github.token }}
        MINIMUM_PRS: "3"
      inputs:
        tag:
          description: Next patch tag, exactly one patch above the latest vMAJOR.MINOR.PATCH tag. Example v1.1.27.
          required: true
          type: string
        pull_requests:
          description: Comma-separated Dependabot pull request numbers to include, with no spaces. Example 120,121,122.
          required: true
          type: string
      steps:
        - name: Checkout main
          uses: actions/checkout@v7
          with:
            ref: main
            fetch-depth: 0

        - name: Validate release request
          id: gate
          run: |
            set -euo pipefail

            if [ "${GH_AW_SAFE_OUTPUTS_STAGED:-}" = "true" ]; then
              echo "Staged mode: release will not be published."
              echo "proceed=false" >> "$GITHUB_OUTPUT"
              exit 0
            fi

            if [ ! -f "${GH_AW_AGENT_OUTPUT:-}" ]; then
              echo "No agent output. Nothing to release."
              echo "proceed=false" >> "$GITHUB_OUTPUT"
              exit 0
            fi

            count="$(jq '[.items[] | select(.type == "create_patch_release")] | length' "$GH_AW_AGENT_OUTPUT")"
            if [ "$count" = "0" ]; then
              echo "Agent did not request a release."
              echo "proceed=false" >> "$GITHUB_OUTPUT"
              exit 0
            fi
            if [ "$count" != "1" ]; then
              echo "Expected exactly one create_patch_release request, got $count."
              exit 1
            fi

            item="$(jq -c '.items[] | select(.type == "create_patch_release")' "$GH_AW_AGENT_OUTPUT")"
            tag="$(echo "$item" | jq -r '.tag')"
            prs="$(echo "$item" | jq -r '.pull_requests')"

            if ! [[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
              echo "Tag must match vMAJOR.MINOR.PATCH, got: $tag"
              exit 1
            fi
            if ! [[ "$prs" =~ ^[0-9]+(,[0-9]+)*$ ]]; then
              echo "pull_requests must be comma-separated numbers, got: $prs"
              exit 1
            fi

            git fetch origin --tags --force
            latest=""
            while IFS= read -r tagref; do
              if [[ "$tagref" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
                latest="$tagref"
                break
              fi
            done < <(git tag -l 'v*' --sort=-v:refname)
            if [ -z "$latest" ]; then
              echo "No existing vMAJOR.MINOR.PATCH tag found."
              exit 1
            fi

            version="${latest#v}"
            major="${version%%.*}"
            rest="${version#*.}"
            minor="${rest%%.*}"
            patch="${rest#*.}"
            expected="v${major}.${minor}.$((patch + 1))"
            if [ "$tag" != "$expected" ]; then
              echo "Refusing tag $tag. The next patch after $latest is $expected."
              exit 1
            fi
            if git rev-parse -q --verify "refs/tags/$tag" >/dev/null; then
              echo "Tag $tag already exists."
              exit 1
            fi
            if gh release view "$tag" >/dev/null 2>&1; then
              echo "Release $tag already exists."
              exit 1
            fi

            normalized="$(echo "$prs" | tr ',' '\n' | sort -n | uniq | paste -sd, -)"
            IFS=',' read -ra numbers <<< "$normalized"
            pr_count="${#numbers[@]}"
            if [ "$pr_count" -lt "$MINIMUM_PRS" ]; then
              echo "Need at least $MINIMUM_PRS Dependabot pull requests, got $pr_count."
              exit 1
            fi

            allowed_path() {
              case "$1" in
                go.mod|go.sum|Dockerfile|.github/dependabot.yaml|.github/dependabot.yml)
                  return 0
                  ;;
                .github/workflows/*.yml|.github/workflows/*.yaml)
                  case "$1" in
                    *.lock.yml) return 1 ;;
                    *) return 0 ;;
                  esac
                  ;;
                *)
                  return 1
                  ;;
              esac
            }

            repo="${GITHUB_REPOSITORY}"
            IFS=',' read -ra numbers <<< "$normalized"
            for number in "${numbers[@]}"; do
              meta="$(gh pr view "$number" --json author,baseRefName,state,isDraft,files,headRepository)"
              author="$(echo "$meta" | jq -r '.author.login')"
              base="$(echo "$meta" | jq -r '.baseRefName')"
              state="$(echo "$meta" | jq -r '.state')"
              draft="$(echo "$meta" | jq -r '.isDraft')"
              head_repo="$(echo "$meta" | jq -r '.headRepository.nameWithOwner')"

              author_lc="$(printf '%s' "$author" | tr '[:upper:]' '[:lower:]')"
              repo_lc="$(printf '%s' "$repo" | tr '[:upper:]' '[:lower:]')"
              head_repo_lc="$(printf '%s' "$head_repo" | tr '[:upper:]' '[:lower:]')"
              if { [ "$author_lc" != "dependabot[bot]" ] && [ "$author_lc" != "app/dependabot" ]; } || [ "$base" != "main" ] || [ "$state" != "OPEN" ] || [ "$draft" != "false" ] || [ "$head_repo_lc" != "$repo_lc" ]; then
                echo "Pull request #$number is not an open Dependabot pull request targeting main in $repo (author=$author base=$base state=$state draft=$draft head=$head_repo)."
                exit 1
              fi

              file_count="$(echo "$meta" | jq '.files | length')"
              if [ "$file_count" -lt 1 ]; then
                echo "Pull request #$number does not change any files."
                exit 1
              fi

              while IFS= read -r path; do
                if ! allowed_path "$path"; then
                  echo "Pull request #$number changes a file outside the dependency allowlist: $path"
                  exit 1
                fi
              done < <(echo "$meta" | jq -r '.files[].path')
            done

            echo "proceed=true" >> "$GITHUB_OUTPUT"
            echo "tag=$tag" >> "$GITHUB_OUTPUT"
            echo "prs=$normalized" >> "$GITHUB_OUTPUT"

        - name: Set up Go
          if: steps.gate.outputs.proceed == 'true'
          uses: actions/setup-go@v7
          with:
            go-version: "1.26"

        - name: Apply Dependabot updates
          if: steps.gate.outputs.proceed == 'true'
          env:
            PRS: ${{ steps.gate.outputs.prs }}
          run: |
            set -euo pipefail
            git config --local user.email "actions@github.com"
            git config --local user.name "GitHub Actions"
            declare -A best_version
            IFS=',' read -ra numbers <<< "$PRS"
            for number in "${numbers[@]}"; do
              git fetch origin "pull/${number}/head:pr-${number}"
              while IFS= read -r path; do
                case "$path" in
                  go.mod|go.sum) ;;
                  *)
                    if ! git diff --quiet origin/main -- "$path"; then
                      echo "Pull request #${number} changes ${path}, which another update already changed."
                      exit 1
                    fi
                    git checkout "pr-${number}" -- "$path"
                    ;;
                esac
              done < <(git diff --name-only origin/main "pr-${number}")

              while IFS= read -r spec; do
                module="${spec%% *}"
                version="${spec#* }"
                current="$(awk -v module="$module" '$1 == module && $2 ~ /^v[0-9]/ { print $2; exit }' <(git show origin/main:go.mod))"
                chosen="$version"
                if [ -n "${best_version[$module]+x}" ]; then
                  chosen="$(printf '%s\n%s\n' "$version" "${best_version[$module]}" | sort -V | tail -n 1)"
                fi
                if [ -n "$current" ]; then
                  newest="$(printf '%s\n%s\n' "$current" "$chosen" | sort -V | tail -n 1)"
                  if [ "$newest" = "$current" ]; then
                    continue
                  fi
                fi
                best_version["$module"]="$chosen"
              done < <(git diff origin/main "pr-${number}" -- go.mod | awk '
                /^\+[^+]/ {
                  sub(/^\+/, "")
                  if ($0 ~ /\/\/ indirect/) next
                  sub(/\/\/.*/, "")
                  gsub(/^[[:space:]]+|[[:space:]]+$/, "")
                  if ($2 ~ /^v[0-9]/) print $1, $2
                }
              ')
            done

            if [ "${#best_version[@]}" -gt 0 ]; then
              args=()
              for module in "${!best_version[@]}"; do
                args+=("${module}@${best_version[$module]}")
              done
              go get "${args[@]}"
              go mod tidy
            fi

            allowed_path() {
              case "$1" in
                go.mod|go.sum|Dockerfile|.github/dependabot.yaml|.github/dependabot.yml)
                  return 0
                  ;;
                .github/workflows/*.yml|.github/workflows/*.yaml)
                  case "$1" in
                    *.lock.yml) return 1 ;;
                    *) return 0 ;;
                  esac
                  ;;
                *)
                  return 1
                  ;;
              esac
            }

            changed="$(git diff --name-only origin/main)"
            if [ -z "$changed" ]; then
              echo "Combined dependency update is empty."
              exit 1
            fi
            while IFS= read -r path; do
              if ! allowed_path "$path"; then
                echo "Combined update changes a file outside the dependency allowlist: $path"
                exit 1
              fi
            done <<< "$changed"
            git add -A
            git commit -m "Apply Dependabot updates"

        - name: Run Go tests
          if: steps.gate.outputs.proceed == 'true'
          run: |
            set -euo pipefail
            go test ./mydump
            go test ./mygzip
            go test ./mys3

        - name: Run integration test
          if: steps.gate.outputs.proceed == 'true'
          run: |
            set -euo pipefail
            docker run -d --name s3dbdump-mariadb \
              -p 3306:3306 \
              -e MYSQL_ROOT_PASSWORD=testpass123 \
              -e MYSQL_DATABASE=test \
              -e MYSQL_USER=test \
              -e MYSQL_PASSWORD=testpass123 \
              mariadb:latest
            docker run -d --name s3dbdump-minio \
              -p 9000:9000 \
              -e MINIO_ACCESS_KEY=minio \
              -e MINIO_SECRET_KEY=minio123 \
              minio/minio:edge-cicd

            DEBIAN_FRONTEND=noninteractive sudo apt-get update -qq
            DEBIAN_FRONTEND=noninteractive sudo apt-get install -y -qq mysql-client

            mkdir -p ~/.mysql
            cat > ~/.mysql/my.cnf << EOF
            [client]
            host=127.0.0.1
            port=3306
            user=root
            password=testpass123
            EOF
            chmod 600 ~/.mysql/my.cnf

            timeout 60 bash -c 'until mysqladmin --defaults-extra-file=~/.mysql/my.cnf ping --silent; do sleep 2; done'
            mysql --defaults-extra-file=~/.mysql/my.cnf test -e "CREATE DATABASE IF NOT EXISTS nudump;"
            mysql --defaults-extra-file=~/.mysql/my.cnf test -e "CREATE DATABASE IF NOT EXISTS nudiff;"
            mysql --defaults-extra-file=~/.mysql/my.cnf nudump < migrations/nudump.sql
            mysql --defaults-extra-file=~/.mysql/my.cnf nudiff < migrations/nudiff.sql

            arch="$(uname -m)"
            case "$arch" in
              x86_64) mc_url="https://dl.min.io/client/mc/release/linux-amd64/mc" ;;
              aarch64|arm64) mc_url="https://dl.min.io/client/mc/release/linux-arm64/mc" ;;
              *) echo "Unsupported architecture: $arch"; exit 1 ;;
            esac
            curl -fL "$mc_url" --create-dirs -o "$HOME/minio-binaries/mc"
            chmod +x "$HOME/minio-binaries/mc"

            timeout 60 bash -c 'until curl -f http://127.0.0.1:9000/minio/health/live; do sleep 2; done'
            "$HOME/minio-binaries/mc" alias set myminio http://127.0.0.1:9000 minio minio123
            "$HOME/minio-binaries/mc" mb myminio/dbdumps
            "$HOME/minio-binaries/mc" policy set public myminio/dbdumps

            docker build --load -t s3dbdump .
            docker volume create s3dbdump-temp
            docker run --rm -v s3dbdump-temp:/tmp alpine:latest chown -R 65534:65534 /tmp
            docker run --rm --name s3dbdump --network host -v s3dbdump-temp:/tmp \
              -e AWS_ACCESS_KEY_ID='minio' \
              -e AWS_SECRET_ACCESS_KEY='minio123' \
              -e S3_ENDPOINT='http://127.0.0.1:9000' \
              -e S3_BUCKET='dbdumps' \
              -e DB_HOST='127.0.0.1' \
              -e DB_PORT='3306' \
              -e DB_USER='root' \
              -e DB_PASSWORD='testpass123' \
              -e DB_ALL_DATABASES='1' \
              -e DB_DUMP_PATH='/tmp' \
              -e DB_DUMP_FILE_KEEP_DAYS='7' \
              s3dbdump

            listing="$("$HOME/minio-binaries/mc" ls myminio/dbdumps)"
            if [[ "$listing" == *.sql.gz* ]]; then
              echo "Integration test passed: found a backup in the MinIO bucket."
            else
              echo "Integration test failed: no backups found in the MinIO bucket."
              exit 1
            fi

        - name: Publish patch release
          if: steps.gate.outputs.proceed == 'true'
          env:
            TAG: ${{ steps.gate.outputs.tag }}
            PRS: ${{ steps.gate.outputs.prs }}
          run: |
            set -euo pipefail
            git merge-base --is-ancestor origin/main HEAD
            git push origin HEAD:main

            {
              echo "Patch release for Dependabot updates."
              echo
              echo "Included pull requests:"
              IFS=',' read -ra numbers <<< "$PRS"
              for number in "${numbers[@]}"; do
                title="$(gh pr view "$number" --json title -q .title)"
                echo "- #${number} ${title}"
              done
              echo
              echo "Go tests for ./mydump, ./mygzip, and ./mys3 passed."
              echo "The integration test passed."
            } > "$RUNNER_TEMP/release-notes.md"

            gh release create "$TAG" --target "$(git rev-parse HEAD)" --title "$TAG" --notes-file "$RUNNER_TEMP/release-notes.md"

            for number in "${numbers[@]}"; do
              gh pr close "$number" --comment "Included in patch release ${TAG}." || true
            done

        - name: Wait for the release image
          if: steps.gate.outputs.proceed == 'true'
          env:
            TAG: ${{ steps.gate.outputs.tag }}
          run: |
            set -euo pipefail
            sha="$(git rev-parse HEAD)"
            echo "Waiting for s3dbdump CI to publish ${TAG} from ${sha}"
            run_id=""
            for _ in $(seq 1 30); do
              run_id="$(gh run list --workflow "s3dbdump CI" --event release --limit 10 --json databaseId,headSha | jq -r --arg sha "$sha" '[.[] | select(.headSha == $sha)][0].databaseId // empty')"
              if [ -n "$run_id" ]; then
                break
              fi
              sleep 10
            done
            if [ -z "$run_id" ]; then
              echo "The release workflow did not start for ${sha}."
              exit 1
            fi

            for _ in $(seq 1 90); do
              conclusion="$(gh run view "$run_id" --json jobs | jq -r '.jobs[] | select(.name == "Build and Push") | .conclusion // empty')"
              if [ "$conclusion" = "success" ]; then
                echo "Image ghcr.io/stenstromen/s3dbdump:${TAG} is published."
                exit 0
              fi
              if [ "$conclusion" = "failure" ] || [ "$conclusion" = "cancelled" ]; then
                echo "Image build finished with ${conclusion}."
                exit 1
              fi
              sleep 20
            done
            echo "Timed out waiting for the image build."
            exit 1

        - name: Checkout flux repository
          if: steps.gate.outputs.proceed == 'true'
          uses: actions/checkout@v7
          with:
            repository: stenstromen/flux
            token: ${{ secrets.KUBE_GITHUB_TOKEN }}
            path: flux-repo
            fetch-depth: 0

        - name: Update Flux cronjob image
          if: steps.gate.outputs.proceed == 'true'
          env:
            TAG: ${{ steps.gate.outputs.tag }}
          run: |
            set -euo pipefail
            cd flux-repo
            git fetch origin
            git checkout main
            cronjob_file="cronjobs/stinky/mariadb-backup-s3.yaml"
            old_image="$(grep "image:" "$cronjob_file" | awk '{print $2}')"
            new_image="ghcr.io/stenstromen/s3dbdump:${TAG}"
            sed -i "s|image:.*|image: ${new_image}|" "$cronjob_file"
            echo "Update image: ${old_image} -> ${new_image}" > "$RUNNER_TEMP/commit_message.txt"
            git config --local user.email "actions@github.com"
            git config --local user.name "GitHub Actions"
            git add cronjobs/stinky/mariadb-backup-s3.yaml
            if git diff --cached --quiet; then
              echo "Flux image tag is already ${new_image}."
              exit 0
            fi
            git commit -F "$RUNNER_TEMP/commit_message.txt"
            if ! git push origin HEAD:main; then
              git pull --rebase origin main
              if git diff --quiet && git diff --cached --quiet; then
                echo "Flux image tag is already ${new_image}."
                exit 0
              fi
              git push origin HEAD:main
            fi
---

# Dependabot patch release

Publish one patch release when several Dependabot updates are waiting. The release job merges those pull requests, runs the tests, and creates the GitHub release only after the tests pass. Do not merge, tag, push, or create the release yourself.

## When to stop

List open pull requests in this repository. Keep a pull request only when all of the following are true:

- The author is `dependabot[bot]` or `app/dependabot`.
- The base branch is `main`.
- It is open and not a draft.
- Every changed file is one of `go.mod`, `go.sum`, `Dockerfile`, `.github/dependabot.yaml`, `.github/dependabot.yml`, or a `.yml` or `.yaml` file under `.github/workflows/`.
- It does not change a `.lock.yml` file.

If fewer than 3 pull requests remain, call `noop` with the count you found and stop. Do not call `create_patch_release`.

## Version

Read the latest git tag that matches `vMAJOR.MINOR.PATCH`. The release tag is that version with only the patch number increased by 1. For example, `v1.1.26` becomes `v1.1.27`. Do not change the major or minor numbers. If you cannot find a tag in that form, call `noop` and stop.

## Publish

Call `create_patch_release` exactly once:

- `tag`: the next patch tag
- `pull_requests`: the selected pull request numbers, comma-separated, with no spaces

The release job checks the tag and the pull requests again. It applies those updates onto `main`. Go module pull requests are combined with `go get` so several `go.mod` bumps do not conflict. It runs `go test` for `./mydump`, `./mygzip`, and `./mys3`, then runs the integration test from `.github/workflows/integration_test.yaml`. It creates the GitHub release only when every test passes. After the release image is published, it sets the Flux cronjob image in `stenstromen/flux` at `cronjobs/stinky/mariadb-backup-s3.yaml` to `ghcr.io/stenstromen/s3dbdump:<tag>`. A failed test publishes nothing and does not update Flux.

Do not call `noop` in the same run as `create_patch_release`.
