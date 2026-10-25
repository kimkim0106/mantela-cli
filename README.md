# mantela-cli

Generate `mantela.json` and Asterisk configuration from your station's configuration files.

You keep extensions and peer stations as TOML files in one station directory. mantela-cli generates the public `mantela.json` (Mantela 0.1.x) and pjsip / dialplan fragments for Asterisk (20 and 22 LTS) from it.

## Status

Early MVP. Only `build` is implemented, covering stage 0 (段階0) of [DESIGN.md](DESIGN.md):

- Extensions: `kind = "device"` only
- Trunks: `[trunk] mode = "register"` only
- No `defaults.toml` yet: write `[pjsip.*]` in each file
- No inter-PBX hop: `[asterisk] hop` is ignored

[samples/](samples/) shows the full design (up to stage 2), so it cannot be built yet. [testdata/sample/station](testdata/sample/station) is the stage 0 subset of it.

## Build

Requires Go 1.24 or later.

```sh
go build
```

## Usage

```sh
mantela-cli -C path/to/station build --mantela mantela.json --asterisk out/asterisk
```

- `-C DIR`: the station directory (default: the current directory)
- `--mantela FILE`: write `mantela.json`
- `--asterisk DIR`: write `pjsip.conf` and `extensions.conf`

At least one of `--mantela` and `--asterisk` is required. Output paths are relative to the current directory, not to `-C`.

Use a directory dedicated to mantela-cli for `--asterisk`. Generated files start with a header carrying the sha256 of their content, and `build` refuses to overwrite a file without the header or one edited by hand. If any check fails, nothing is written.

Put the generated files on the PBX (for example in `/etc/asterisk/mantela-cli/`) and include them from your own configuration:

```ini
; /etc/asterisk/pjsip.conf
#include mantela-cli/pjsip.conf

; /etc/asterisk/extensions.conf
#include mantela-cli/extensions.conf
```

This version does not handle passwords: they appear in neither the station directory nor the generated files, so both are safe to commit. Write them by hand in the secrets file (`[asterisk] secrets`, default `/etc/asterisk/pjsip_secrets.conf`) on the PBX. The generated `pjsip.conf` includes that file and inherits from templates in it: `secret-<extension>` for extensions and `secret-trunk-<slug>` for trunks.

```ini
[secret-6001](!)
password = xxxxx
```

## Development

```sh
go test ./...
go test -run TestGolden . -update   # rewrite the expected files in testdata/*/want
```

## Reference

- https://github.com/tkytel/mantela

## License

[MIT](LICENSE)
