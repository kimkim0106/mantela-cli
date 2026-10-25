# samples

DESIGN.md の設計どおりに作った、Home Lab の局を想定した例です。mantela-cli はまだ実装されていないので、`out/` の生成物は手で書いた「期待する出力」です。名前・番号・アドレス・パスワードはすべて架空の値です。

```
samples/
├── station/                  # 局ディレクトリ（人間が書く。git で管理する想定）
│   ├── station.toml
│   ├── defaults.toml
│   ├── extensions/
│   │   ├── 117.toml          # 時報（dialplan）
│   │   ├── 1234.toml         # エコーテスト（dialplan）
│   │   ├── 6000.toml         # 全台呼び出し（group）
│   │   ├── 6001.toml         # 黒電話
│   │   ├── 6002.toml         # スマートフォン
│   │   ├── 6003.toml         # FAX（identifier を省略した例）
│   │   └── 6009.toml         # 検証用（Mantela に載せない）
│   └── providers/
│       ├── station-a.toml    # mode = "register"
│       ├── station-b.toml    # enabled = false（保守中）
│       └── station-c.toml    # mode = "ip"、strip_prefix = false
├── out/                      # mantela-cli build の出力（期待値）
│   ├── mantela.json
│   └── asterisk/             # PBX の /etc/asterisk/mantela-cli/ に置く
│       ├── pjsip.conf
│       ├── extensions.conf
│       └── secrets.required
└── pbx/etc/asterisk/         # mantela-cli の外で用意するファイルの一例（手書きでも別のツールでもよい）
    ├── pjsip.conf            # transport の定義と、生成物の #include
    ├── extensions.conf       # 生成物の #include と、dialplan の Goto 先
    └── pjsip_secrets.conf    # パスワード（ダミー）
```

## 流れ

```sh
mantela-cli -C samples/station build --mantela samples/out/mantela.json --asterisk samples/out/asterisk
```

1. `station/` を読み、`out/` を作る
2. `out/asterisk/` を PBX の `/etc/asterisk/mantela-cli/` に置く
3. PBX の `pjsip.conf` / `extensions.conf` が、それぞれ1行の `#include` で生成物を読み込む
4. 生成した `pjsip.conf` は、先頭で `/etc/asterisk/pjsip_secrets.conf` を読み込み、auth はそこのテンプレート（`secret-6001` など）を継承する
5. `out/mantela.json` を Web で公開する

## 見どころ

- `defaults.toml` と要素ファイルの合成とキーの順序: `6001` は `dtmf_mode` の値だけを上書きし（位置は `defaults.toml` のまま）、`6003` は `allow` を配列ごと上書きして、`defaults.toml` に無い `t38_udptl` を後ろに足している
- 公開と生成の違い: `6009` は `publish = false` なので `mantela.json` には無いが、Asterisk の設定は作る。`station-b` は `enabled = false` なので Asterisk の設定は作らず、`mantela.json` には `unavailable: true` で載る
- identifier: `6003` は省略したので `mantela.json` に identifier が無い。group の members（`6001`、`6002`）は identifier が要る
- 生成物の先頭の印: `; sha256:` の値は、その行より後ろの本文の sha256
- `pbx/` と局ディレクトリの結びつき: `defaults.toml` の `transport = "transport-udp"`、`dialplan` の `target`（`echo-test`、`jihou`）、secrets のテンプレート名は、`pbx/` の同名の定義を参照している（DESIGN.md「PBX 側に求めるもの」）

## 段階ごとに使える範囲

このサンプルは、段階2までの機能を使った完成形です（DESIGN.md「段階的な実装」）。MVP（段階0）で扱えるのは一部だけです。

| 段階 | このサンプルで当たるもの |
|---|---|
| 段階0（MVP） | `station.toml`、内線 `6001` / `6002` / `6003` / `6009`（`kind = "device"`）、`station-a`（`mode = "register"`）、`[pjsip.*]` の素通しとキーの順序、secrets のテンプレート、生成物の印 |
| 段階1 | `defaults.toml`（MVP では、各ファイルに `[pjsip.*]` を直接書く）、`secrets.required` |
| 段階2 | `6000`（group）、`117` / `1234`（dialplan）、`station-c`（`mode = "ip"`）、`station-b`（`enabled = false` から決まる `unavailable`）、`6001` の素通し用 `[mantela]`、`mantela-hop`（MVP では `hop` を無視するので生成しない） |

サーバ側での手作業の調整（生成物を直さずに `pbx/` の側で項目を足すなど）は、当面は扱いません。`pbx/` のファイルは、生成物を読み込むための最小限の例です。
