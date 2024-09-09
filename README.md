# Caddy Postgres Config Adapter

This is a [config adapter](https://caddyserver.com/docs/config-adapters) for Caddy which Store And Update Configuration.

- This project is not complete, and we are asking the community to help finish its development.

Currently supported key in postgres table:

- version
- config (now all the configuration is in config key, should be separate to others in the future,welcome to create Pull Request https://caddyserver.com/docs/json/ https://caddyserver.com/docs/caddyfile/options)

Thank you, and we hope you have fun with it!

## Install

First, the [xcaddy](https://github.com/caddyserver/xcaddy) command:

```shell
$ go get -u github.com/caddyserver/xcaddy/cmd/xcaddy
```

Then build Caddy with this Go module plugged in. For example:

```shell
$ xcaddy build --with github.com/fresent/caddy-postgres-adapter
```

## Use

Using this config adapter is the same as all the other config adapters.

- [Learn about config adapters in the Caddy docs](https://caddyserver.com/docs/config-adapters)
- You can adapt your config with the [`adapt` command](https://caddyserver.com/docs/command-line#caddy-adapt)

```
caddy run --adapter postgres --config ./postgres.json
```

- postgres.json

```
{
  "connection_string": "postgres://user:password@localhost:5432/postgres?sslmode=disable"
  "dbname": "caddyconfigadapter",
  "host": "localhost",
  "user": "postgres",
  "password": "postgres",
  "port": "5432",
  "sslmode": "disable",
  "disable_ddl": false,
  "query_timeout": 120000000, //in nanoseconds
  "lock_timeout": 120000000, //in nanoseconds
  "tableNamePrefix": "CADDY", //table prefix in postgres ,full table name should be CADDY_CONFIG
  "refreshInterval": 3 //in seconds ,  auto check version in  CADDY_CONFIG table,reload caddy server if the version updated.
}
```

- table schema (it should be created atomically)

![This is an image](./table_demo.jpg)

- table DDL should like below

```SQL
CREATE TABLE `CADDY_CONFIG` (
  `id` SERIAL PRIMARY KEY,
  `key` VARCHAR(255) NOT NULL,
  `value` TEXT,
  `enable` BOOLEAN NOT NULL DEFAULT TRUE,
  `created` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated` tTIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS `CADDY_CONFIG_key_idx` ON `CADDY_CONFIG` (`key`);
```

- table data should be

```
INSERT INTO `CADDY_CONFIG` (`id`,`key`,`value`,`enable`,`created`,`updated`) VALUES (NULL,'version','15',1,NULL,NULL);
INSERT INTO `CADDY_CONFIG` (`id`,`key`,`value`,`enable`,`created`,`updated`) VALUES (NULL,'config','{\"apps\":{\"http\":{\"http_port\":80,\"https_port\":443,\"servers\":{\"srv0\":{\"listen\":[\":80\"],\"routes\":[{\"handle\":[{\"handler\":\"subroute\",\"routes\":[{\"handle\":[{\"body\":\"Hello, world!\",\"handler\":\"static_response\"}]}]}],\"match\":[{\"host\":[\"localhost\"]}],\"terminal\":true},{\"handle\":[{\"handler\":\"subroute\",\"routes\":[{\"handle\":[{\"body\":\"Hello, world!\",\"handler\":\"static_response\"}]}]}],\"match\":[{\"host\":[\"localhost3\"]}],\"terminal\":true}]}}}},\"logging\":{\"logs\":{\"default\":{\"level\":\"DEBUG\"}}},\"storage\":{\"connection_string\":\"postgres://caddy:caddy@localhost:5432/caddy?sslmode=disable\",\"module\":\"postgres\"}}',1,NULL,NULL);
INSERT INTO `CADDY_CONFIG` (`id`,`key`,`value`,`enable`,`created`,`updated`) VALUES (NULL,'config.admin','{\n    \"listen\": \"localhost:2019\"\n  }',1,NULL,NULL);
INSERT INTO `CADDY_CONFIG` (`id`,`key`,`value`,`enable`,`created`,`updated`) VALUES (NULL,'config.storage',' {\n    \"connection_string\": \"postgres://caddy:caddy@localhost:5432/caddy?sslmode=disable\",\n    \"module\": \"postgres\"\n  }',1,NULL,NULL);
INSERT INTO `CADDY_CONFIG` (`id`,`key`,`value`,`enable`,`created`,`updated`) VALUES (NULL,'config.logging','{\n    \"logs\": {\n      \"default\": {\n        \"level\": \"DEBUG\",\n        \"writer\": {\n          \"filename\": \"./logs/access.log\",\n          \"output\": \"file\",\n          \"roll_keep\": 5,\n          \"roll_keep_days\": 30,\n          \"roll_size_mb\": 954\n        }\n      }\n    }\n  }',1,NULL,NULL);
INSERT INTO `CADDY_CONFIG` (`id`,`key`,`value`,`enable`,`created`,`updated`) VALUES (NULL,'config.apps','{\n    \"http\": {\n      \"http_port\": 80,\n      \"https_port\": 443,\n      \"servers\": {\n        \"srv0\": {\n          \"listen\": [\n            \":80\"\n          ],\n          \"routes\": [\n            {\n              \"handle\": [\n                {\n                  \"handler\": \"subroute\",\n                  \"routes\": [\n                    {\n                      \"handle\": [\n                        {\n                          \"body\": \"Hello, world!\",\n                          \"handler\": \"static_response\"\n                        }\n                      ]\n                    }\n                  ]\n                }\n              ],\n              \"match\": [\n                {\n                  \"host\": [\n                    \"localhost\"\n                  ]\n                }\n              ],\n              \"terminal\": true\n            }\n          ]\n        }\n      }\n    }\n  }',1,NULL,NULL);
INSERT INTO `CADDY_CONFIG` (`id`,`key`,`value`,`enable`,`created`,`updated`) VALUES (NULL,'config.apps.http.servers.srv0.routes','{\n  \"handle\": [\n    {\n      \"handler\": \"subroute\",\n      \"routes\": [\n        {\n          \"group\": \"group0\",\n          \"handle\": [\n            {\n              \"handler\": \"rewrite\",\n              \"uri\": \"/jonz\"\n            }\n          ],\n          \"match\": [\n            {\n              \"path\": [\n                \"/\"\n              ]\n            }\n          ]\n        },\n        {\n          \"handle\": [\n            {\n              \"handler\": \"reverse_proxy\",\n              \"headers\": {\n                \"request\": {\n                  \"set\": {\n                    \"Host\": [\n                      \"{http.reverse_proxy.upstream.hostport}\"\n                    ]\n                  }\n                }\n              },\n              \"transport\": {\n                \"protocol\": \"http\",\n                \"tls\": {}\n              },\n              \"upstreams\": [\n                {\n                  \"dial\": \"google.com:443\"\n                }\n              ]\n            }\n          ]\n        }\n      ]\n    }\n  ],\n  \"match\": [\n    {\n      \"host\": [\n        \"google.proxy.vip\"\n      ]\n    }\n  ],\n  \"terminal\": true\n}',1,NULL,NULL);


```

- postgres rows data means
  - `key` == "version" when you have a row with `key` if the `value` changed or added , the caddyserver should reload configuration in refreshInterval
  - `key` == "config" you can store the caddy json config in value completely.
  - `key` == "admin" admin json value in https://caddyserver.com/docs/json/
  - `key` == "storage" storage json value in https://caddyserver.com/docs/json/
  - `key` == "logging" logging json value in https://caddyserver.com/docs/json/
  - `key` == "apps" apps json value in https://caddyserver.com/docs/json/
  - `key` == "config.apps.http.servers.srv0.routes" if you have srv0 in config.apps.http.servers then you can add multiple config.apps.http.servers.srv0.routes rows , one row may be means a http host which to be access from browser, do not forget update the `version` after add or changed row value.
