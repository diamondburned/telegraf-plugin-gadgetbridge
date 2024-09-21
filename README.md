# telegraf-plugin-gadgetbridge

Telegraf plugin that ingests data from Gadgetbridge's auto-export file and
sends it to Telegraf.

## Usage

Configuring Telegraf:

```toml
[[inputs.execd]]
  command = ["telegraf-plugin-gadgetbridge", "-config", "/path/to/config.toml"]
  signal = "none"
```

Configuring the Gadgetbridge plugin:

```toml
[[inputs.gadgetbridge]]
  ## Path to the Gadgetbridge auto-export file(s).
  database_paths = ["/path/to/gadgetbridge-export.db"]
```
