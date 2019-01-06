# Kushi: SSH Client with auto-sync SSH port fowarding settings.

## What is this?

SSH Client + autossh + sync portfowarding settings on `.ssh/config`

## Usage

### 1. Create Kushi configs

```
% emacs ~/.config/kushi/config.yml
```

```yaml
BindingConfigsURL: https://s3-ap-northeast-1.amazonaws.com/path_to_your_binding_list.yaml (required)
CheckInterval: 600 (seconds, optional, default = 600)
SSHConfig:
  HostName: your_ssh_server_hostname (required)
  User: your_ssh_username (required)
  IdentityFile: path_to_your_ssh_identity_key_file (optional, default = $HOME/.ssh/id_ed25519, $HOME/.ssh/id_rsa)
  Port: your_ssh_server_port (optional, default = 22)
  KeepaliveInterval: 3 (seconds, optional, default = 3)
  Timeout: 5 (seconds, optional, default = 5)
```

### 2. Place the portfowarfing bindings list file on your managed server (like S3)

```
https://s3-ap-northeast-1.amazonaws.com/path_to_your_binding_list.yaml
```

```yaml
# WEB
- src: localhost:18080
  dst: your_awesome_server_host:8080

# DB
- src: localhost:13306
  dst: your_awesome_mysql_server_host:3306
```

### 3. Run kushi

```
% kushi --stdout
```

### 4. (Optional) Run as daemon

```
% nohup /Users/cnosuke/dev/bin/kushi 1>/dev/null 2>&1 &
```

All logs are written to `~/.config/kushi/logs/20180128172917.log` .
(Filename is a timestamp of process started time.)

## LICENSE

MIT license

Copyright (C) 2018 Shinnosuke Takeda a.k.a `@cnosuke`
