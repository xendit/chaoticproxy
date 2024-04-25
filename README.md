# Behold! The Chaotic Proxy!

This is a simple TCP-based Proxy that is used to intercept connections between client and servers and add a bit of chaos to the communication. That is designed to be used in test environments to simulate network issues and test the application's resilience.

# Configuration

The proxy is configured using a JSON file. By default, the file is name `config.json`. It defines a collection of listeners, each one with a its own configuration for listening and target addresses as well as chaos rules.

Example configuration:

```json
{
  "listeners": [
    {
      "name": "http",
      "address": "127.0.0.1:8080",
      "target": "127.0.0.1:80",
      "rejectionRate": 0.02,
      "durability": {
        "mean": 120.0,
        "stddev": 30.0
      },
      "latency": {
        "mean": 0.1,
        "stddev": 0.05
      }
    }
  ]
}
```

This configuration defines one single listener:

- `name`: The name of the listener. It is used to identify the listener in the logs. It can be set to any string.
- `address`: The address where the proxy will listen for incoming connections. It is a string in the format `host:port`.
- `target`: The address where the proxy will forward the connections. It is a string in the format `host:port`.
- `rejectionRate`: The rate of connections that will be rejected by the proxy. It is a float between 0 and 1. A value of 0 means that no connections will be rejected. A value of 1 means that all connections will be rejected.
- `durability`: How long should the connection be allowed to remain open until "chaos" is applied and the connection is closed. It is defined by a mean and a standard deviation. The values are in seconds. Set both to 0 to disable this feature (i.e. the connection will never be closed by the proxy).
- `latency`: How long should the proxy delay the packets. It is defined by a mean and a standard deviation. The values are in seconds. Set both to 0 to disable this feature (i.e. the packets will not be delayed). Latency is applied on both directions of the connection.

# Updating the configuration

The proxy will monitor the configuration file and changes will be applied without the need to restart the proxy. If the configuration file is deleted, the proxy will keep running with the last configuration that was loaded.

Chaos configuration changes will only affect new connections. Existing connections will keep using the configuration that was in place when they were established.

# Latency logic and limitations

The proxy acts on top of TCP, so latency is only applied within the existing TCP stream. It does not work by delaying IP packets or anything fancy like this.

Instead, the proxy will always accept any data that is sent by the client and will only forward it to the target after the latency time has passed. This means that the client will always be able to send data to the proxy, but the target will only receive it after the latency time has passed.

This will give the client the wrong impression that the data has been received by the server, whereas it is not necessarily the case. This is a limitation of this proxy's implementation.

# Building

To build the proxy, you need to have Go installed. Then, you can run the following command:

```sh
go build
```

This will generate an executable named `chaoticproxy` (or `chaoticproxy.exe` on Windows). Simply run this executable to start the proxy.

# Running

To run the proxy, you need to provide a configuration file. By default, the proxy will look for a file named `config.json` in the current directory.

```sh
./chaoticproxy
```

You can also provide a different file by using the `-config-file` flag:

```sh
./chaoticproxy -config-file /path/to/config.json
```

Two alternatives to provide the configuration are:

- Use the `-config-str` flag to provide the configuration as a JSON string directly over the command-line.

```sh
./chaoticproxy -config-str '{"listeners":[{"name":"http","address":"localhost:8080","target":"localhost:80"}]}'
```

- Use the `-config-env` flag to provide the configuration as a JSON string stored in an environment variable.

```sh
export CHAOTICPROXY_CONFIG='{"listeners":[{"name":"http","address":"localhost:8080","target":"localhost:80"}]}'
./chaoticproxy -config-env CHAOTICPROXY_CONFIG
```

# License

This project is licensed under the BSD 3 License. See the [LICENSE](LICENSE) file for details.
