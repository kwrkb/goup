# goup

Go 言語本体（toolchain）を更新する CLI。`/usr/local/go` に展開された公式 tarball を安全に入れ替える。

## 設計方針

- **stdlib-only**: 外部依存パッケージを追加しない。`net/http`, `encoding/json`, `crypto/sha256`, `archive/tar`, `compress/gzip`, `os`, `os/exec`, `flag`, `testing`, `net/http/httptest` のみ使用
- **単一静的バイナリ**: `go build` でそのまま配布可能な単一バイナリにする
- **クロスプラットフォーム**: 対象は WSL2 (Ubuntu) と macOS (Apple Silicon)。Windows ネイティブは非対応（`runtime.GOOS == "windows"` を検出したら明示メッセージを出して終了するのみ）
- **フレームワーク不使用**: サブコマンド dispatch は標準 `flag` パッケージで実装する。Cobra 等は使わない
- **対話 TTY のみ自動 sudo 昇格を試みる (v0.3.0〜)**: 書き込みコマンドが権限不足を検知した場合、以下 2 条件を両方満たすときのみ `sudo` で自己再実行する: (a) `/dev/tty` が open 可能（=controlling terminal がある）、(b) stdin が character device である。パスワード入力は `sudo` 自身のプロンプトに委ねる（goup 独自の password 収集はしない）。非対話環境（CI / cron / detached script → (a) 落ち）、パイプ（`| goup`）や regular-file redirect（`goup < file`）→ (b) 落ち、`--no-sudo` フラグ指定時は `hint: rerun with sudo` を出して fast-fail する。既知の pathological case: `goup update < /dev/null` は /dev/null 自体が character device なので昇格側に流れる（stdlib-only 制約下で isatty(3) 相当を書かないための受容トレードオフ、スクリプトは `--no-sudo` を明示すること）。v0.2.0 までは全ケースで fast-fail のみだった
- **コメントは英語で書く**

## コマンド

- `goup check`: 現在バージョンと最新安定版を比較表示するのみ（副作用なし）
- `goup update`: ダウンロード → sha256 検証 → バックアップ → 展開 → 起動確認。起動確認に失敗したら自動ロールバック
- `goup rollback`: 直前のバックアップ（`/usr/local/go.bak.<unixtimestamp>`）から手動復元

## バックアップの世代管理

最新 1 世代のみ保持する。新しい `update` を実行する際、既存の `go.bak.*` は新しいバックアップを作る前に削除する。

## テスト容易性

`installer.go` の中核関数は `installRoot`（デフォルト `/usr/local`）と `baseURL`（デフォルト `https://go.dev/dl`）を引数に取る。テストでは `httptest.Server` と `t.TempDir()` を渡すことで、実際の `/usr/local` に触れずに sha256 検証・自動ロールバック・世代管理の挙動を証明する。

## CurrentVersion は VERSION ファイルを直読みする

`CurrentVersion(installRoot)` は `<installRoot>/go/VERSION` の1行目を返す。以前は `go env GOVERSION` を叩いていたが、次の理由でファイル直読みに切り替えた:

- **sudo での PATH 剥奪**: Ubuntu の secure_path は sudo 実行時に `$PATH` を `/usr/sbin:/usr/bin:/sbin:/bin` にリセットするため、`/usr/local/go/bin` にある `go` バイナリが解決できず `sudo goup update` が起動時に失敗していた
- **go.mod toolchain directive との干渉**: カレントディレクトリの `go.mod` に `toolchain` 指定があると `go env GOVERSION` は自動 fetch された別バージョンを返すことがあった（VERSION ファイル読み取りではそもそも関係ない）
- **副次効果**: `go` バイナリが壊れていても（rollback 直前など）installRoot のバージョンが読み取れる

## リリース前チェック

- `go build ./...`
- `go vet ./...`
- `go test ./...`
- `govulncheck ./...`

## リリースビルド

タグ埋め込みは `-ldflags "-X main.version=<tag>"` で行う。`goup version` がリリース済みバイナリで正しいタグを返すのは、このフラグを付けた場合のみ。

```
GOOS=linux  GOARCH=amd64 go build -ldflags="-s -w -X main.version=v0.2.0" -o dist/goup-linux-amd64 .
GOOS=darwin GOARCH=arm64 go build -ldflags="-s -w -X main.version=v0.2.0" -o dist/goup-darwin-arm64 .
```

`-s -w` はデバッグシンボル除去でサイズを 9MB → 6MB 程度に落とす。
