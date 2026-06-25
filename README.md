# shopservatory

This is a service that watches various (second-hand) marketplaces for new listings and displays them in a nice aggregated manner.

Notifications are possible using Telegram currently.

## Install

Here's how to do it using NixOS:

```nix
imports = [ inputs.<shopservatory input name>.nixosModules.default ];
services.shopservatory = {
  enable = true;
  flaresolverr.enable = true; # if you need to pass challenges
  environmentFile = "<path to your secrets env>";
};
```

If you are not using NixOS, I trust you know how to set this up :)

## Contributing

Contributions, for example to support new sources, are very welcome!
