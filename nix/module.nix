{ config
, lib
, pkgs
, ...
}:

let
  cfg = config.services.shopservatory;
  format = pkgs.formats.toml { };
  configFile = format.generate "shopservatory.toml" cfg.settings;

  port = lib.toInt (lib.last (lib.splitString ":" cfg.settings.server.listen));
in
{
  options.services.shopservatory = {
    enable = lib.mkEnableOption "shopservatory, asecond-hand shopping monitor";

    user = lib.mkOption {
      type = lib.types.str;
      default = "shopservatory";
      description = "User the service runs as.";
    };

    group = lib.mkOption {
      type = lib.types.str;
      default = "shopservatory";
      description = "Group the service runs as.";
    };

    stateDir = lib.mkOption {
      type = lib.types.str;
      default = "shopservatory";
      description = "Directory name under /var/lib used for the SQLite database.";
    };

    environmentFile = lib.mkOption {
      type = lib.types.nullOr lib.types.path;
      default = null;
      example = "/run/secrets/shopservatory.env";
      description = ''
        Path to an EnvironmentFile holding secrets.

        Recognised variables:
          SHOPSERVATORY_TELEGRAM_TOKEN
          SHOPSERVATORY_EBAY_CLIENT_ID
          SHOPSERVATORY_EBAY_CLIENT_SECRET
          SHOPSERVATORY_OIDC_CLIENT_SECRET
          SHOPSERVATORY_USER_<NAME>_PASSWORD
          SHOPSERVATORY_USER_<NAME>_CHAT_ID

        <NAME> is the account's name upper-cased with non-alphanumerics
        replaced by "_" (e.g. name = "admin" -> SHOPSERVATORY_USER_ADMIN_PASSWORD).
      '';
    };

    openFirewall = lib.mkOption {
      type = lib.types.bool;
      default = false;
      description = "Open the default TCP port in the firewall.";
    };

    browser = {
      enable = lib.mkOption {
        type = lib.types.bool;
        default = true;
        description = "Enable the headless-browser fetch path required by bot-protected sources.";
      };

      package = lib.mkOption {
        type = lib.types.package;
        default = pkgs.chromium;
        defaultText = lib.literalExpression "pkgs.chromium";
        description = "The Chromium/Chrome package used for browser-backed sources.";
      };
    };

    flaresolverr = {
      enable = lib.mkOption {
        type = lib.types.bool;
        default = false;
        description = ''
          Whether to run a local FlareSolverr instance
          and route Cloudflare-protected sources through it.
        '';
      };

      port = lib.mkOption {
        type = lib.types.port;
        default = 8191;
        description = "Port for the managed FlareSolverr instance.";
      };
    };

    settings = lib.mkOption {
      inherit (format) type;
      default = { };
      example = lib.literalExpression ''
        {
          server.listen = "0.0.0.0:8080";
          scrape.default_interval = "10m";
          ebay.marketplace = "EBAY_DE";
        }
      '';
      description = ''
        Contents of shopservatory.toml.

        See the example file for a list of available values.
      '';
    };
  };

  config = lib.mkIf cfg.enable {
    services.shopservatory.settings = {
      server.listen = lib.mkDefault "127.0.0.1:8080";
      database.path = lib.mkDefault "/var/lib/${cfg.stateDir}/shopservatory.db";
      log.level = lib.mkDefault "info";
      scrape = lib.mkMerge [
        (lib.mkIf cfg.browser.enable {
          browser_path = lib.mkDefault (lib.getExe cfg.browser.package);
        })
        (lib.mkIf cfg.flaresolverr.enable {
          flaresolverr_url = lib.mkDefault "http://127.0.0.1:${toString cfg.flaresolverr.port}";
        })
      ];
    };

    services.flaresolverr = lib.mkIf cfg.flaresolverr.enable {
      enable = true;
      inherit (cfg.flaresolverr) port;
    };

    users.users = lib.mkIf (cfg.user == "shopservatory") {
      shopservatory = {
        isSystemUser = true;
        inherit (cfg) group;
        description = "shopservatory service user";
      };
    };
    users.groups = lib.mkIf (cfg.group == "shopservatory") {
      shopservatory = { };
    };

    networking.firewall = lib.mkIf cfg.openFirewall {
      allowedTCPPorts = [ port ];
    };

    systemd.services.shopservatory = {
      description = "shopservatory – second-hand shopping monitor";
      wantedBy = [ "multi-user.target" ];
      after = [ "network-online.target" ] ++ lib.optional cfg.flaresolverr.enable "flaresolverr.service";
      wants = [ "network-online.target" ] ++ lib.optional cfg.flaresolverr.enable "flaresolverr.service";

      serviceConfig = {
        ExecStart = "${lib.getExe cfg.package} --config ${configFile}";
        User = cfg.user;
        Group = cfg.group;
        StateDirectory = cfg.stateDir;
        StateDirectoryMode = "0750";
        WorkingDirectory = "/var/lib/${cfg.stateDir}";
        Restart = "on-failure";
        RestartSec = 5;
        EnvironmentFile = lib.optional (cfg.environmentFile != null) cfg.environmentFile;
        Environment = lib.optional cfg.browser.enable "HOME=/var/lib/${cfg.stateDir}";

        CapabilityBoundingSet = "";
        AmbientCapabilities = "";
        NoNewPrivileges = true;
        LockPersonality = true;
        MemoryDenyWriteExecute = !cfg.browser.enable;
        PrivateTmp = true;
        PrivateDevices = true;
        ProtectClock = true;
        ProtectControlGroups = true;
        ProtectHome = true;
        ProtectHostname = true;
        ProtectKernelLogs = true;
        ProtectKernelModules = true;
        ProtectKernelTunables = true;
        ProtectProc = "invisible";
        ProcSubset = "pid";
        ProtectSystem = "strict";
        RestrictAddressFamilies = [
          "AF_INET"
          "AF_INET6"
          "AF_UNIX"
        ];
        RestrictNamespaces = true;
        RestrictRealtime = true;
        RestrictSUIDSGID = true;
        SystemCallArchitectures = "native";
        SystemCallFilter =
          if cfg.browser.enable then
            [ "@system-service" ]
          else
            [
              "@system-service"
              "~@privileged"
              "~@resources"
            ];
        UMask = "0077";
      };
    };
  };
}
