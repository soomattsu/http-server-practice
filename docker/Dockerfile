# syntax=docker/dockerfile:1

# BuildKit: ビルドの並列実行/キャッシュimport・export/マルチプラットフォーム対応が可能なビルドエンジン

# BuildKitの組み込み引数
# - TARGETPLATFORM: マルチプラットフォームビルド時に--platformフラグで指定するos/arch
#   - Dockerfile内では、TARGETOS / TARGETARCHとして別々に参照できる
# - BUILDPLATFORM: BuildKit実行マシンのos/arch

# "FROM --platform=$BUILDPLATFORM"の意味
# - 「TARGETPLATFORMに関わらず、そのステージのbase imageのos/archをBUILDPLATFORMのos/archに固定する」という意味
# - デフォルトでは、TARGETPLATFORM != BUILDPLATFORMなビルドの際は、target用os/archをQEMUエミュレーター上で動かす分重くなる
# - Goは言語側でクロスコンパイルサポートが厚く、実行時os/archと出力用os/archを容易に分離できるため、エミュ削減による高速化を享受できる
# - 特に、コンパイルなどの計算処理が重たいbuilder stageでの恩恵が大きい

FROM --platform=$BUILDPLATFORM golang:1.26.6 AS builder
WORKDIR /src
COPY go.mod go.sum ./
# BuildKitのcache mount
# - そのRUN実行中のみ、BuildKit管理下のキャッシュ用永続ボリュームをtargetへマウントする
#   - ビルド中のみ可視化される外部ボリューム
# - BuildKitはid/targetのpathをキーとしてcache mountのボリュームを管理している
#   - image layerとはライフサイクルが完全に切り離されており、使用したRUNのlayerにも含まれない！
# 依存解決時の違い
# - 通常のlayerキャッシュ
#   - go.mod/go.sum更新 = 命令の入力が少しでも変化すると、後続のlayerキャッシュをすべて無効化する
#   -> 依存を1つでも変更すると、COPY以降のlayerキャッシュが切れた状態で再downloadするので、毎回ゼロからfull downloadされてしまう
# - cache mount
#   - COPYのlayerキャッシュが切れて再downloadが必要だとしても、ボリュームを参照して依存解決するので、差分のみのダウンロードが可能
#   -> 依存解決時にキャッシュマウントを使うことで、Goのモジュールキャッシュでの差分検知を活かせるようになる！
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked \
    go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
ARG TARGETARCH
ARG SERVICE_NAME
# RUNの内容
# - コンパイル実行時、外部パッケージのコードの所在（モジュールキャッシュ）としてcache mountを参照
#   - go.mod/go.sumから必要なパッケージを特定し、モジュールキャッシュ内に存在すればdownloadをskipする
#   - go mod download結果をcache mountに書き出したので、go build時に明示的に参照しないとモジュールキャッシュが効かない
# - コンパイル実行時、過去のコンパイル結果の所在（ビルドキャッシュ）としてcache mountを参照
# 　 - ビルドキャッシュ: build実行時、key:value = <hash>:<コンパイル済みアーカイブ>の構造で、pkg毎のbuild結果をGOCACHEへ保存する
# 　　- キャッシュヒット: build実行時、pkg毎にhash計算して、キャッシュ中にhashと一致するキーがあれば、そのpkgのbuildをskipする
# - cgo（GoからCライブラリを呼ぶ仕組み）を無効化 -> libcへの動的リンクを無効化し、ランタイムにlibcを持たないimageを選べるようになる
# - GOARCHでTARGETPLATFORMに応じたバイナリを出力
# - コンパイル後のリンク工程でのメタデータ付与を抑制 -> delveなどのライブデバッグツール用情報を削減してバイナリサイズ縮小
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOARCH=$TARGETARCH go build -ldflags="-s -w" -o /out/server ./cmd/${SERVICE_NAME}-api

# 静的リンク前提＆シェルなしの軽量イメージ
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /out/server /server
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT [ "/server" ]
