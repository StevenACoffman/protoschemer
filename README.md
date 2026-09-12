<!-- rumdl-disable-next-line MD063 -->
# protoschemer

Generate protobuf definitions from schema documents.

## Install

```sh
go install github.com/StevenACoffman/protoschemer@latest
```

Requires Go 1.27 or later.

## Usage

```sh
protoschemer jsonschema \
  --in ./schemas \
  --out ./proto/oneroster/v1p2/v1 \
  --proto-package oneroster.v1p2.v1 \
  --go-package github.com/example/gen/oneroster/v1p2/v1 \
  --import-prefix oneroster/v1p2/v1 \
  --strip-suffix DType
```

protoschemer reads every `*.json` file under `--in` and writes one `.proto` per
message. It pools all `definitions` into one namespace, so a type that two
documents declare alike yields a single message.

Use `--dry-run` to print the result instead of writing it.

## Example

A schema like this:

```json
{
  "definitions": {
    "OrgDType": {
      "type": "object",
      "required": ["sourcedId"],
      "properties": {
        "sourcedId": { "type": "string" },
        "dateLastModified": { "type": "string", "format": "date-time" },
        "active": { "type": "string", "enum": ["true", "false"] },
        "type": {
          "anyOf": [
            { "type": "string", "enum": ["school", "district"] },
            { "type": "string", "pattern": "(ext:)[a-z]+" }
          ]
        }
      }
    }
  }
}
```

protoschemer writes `org.proto`:

```protobuf
syntax = "proto3";
package oneroster.v1p2.v1;
import "google/protobuf/timestamp.proto";
message Org {
  optional bool active = 1;
  google.protobuf.Timestamp date_last_modified = 2;
  string sourced_id = 3;
  OrgType type = 4;
  // Set instead of type when the value falls outside the enumeration above.
  optional string type_ext = 5;
}
enum OrgType {
  ORG_TYPE_UNSPECIFIED = 0;
  ORG_TYPE_SCHOOL = 1;
  ORG_TYPE_DISTRICT = 2;
}
```

## Type Mapping

| JSON Schema                               | Protobuf                            |
| ----------------------------------------- | ----------------------------------- |
| `format: date-time`                       | `google.protobuf.Timestamp`         |
| `format: date`                            | `google.type.Date`                  |
| `enum: ["true","false"]`                  | `bool`                              |
| `additionalProperties` with no properties | `google.protobuf.Struct`            |
| `anyOf: [enum, "ext:" pattern]`           | An enum plus a `<field>_ext` string |
| `$ref`                                    | The referenced message              |
| `type: array`                             | `repeated`                          |
| Property absent from `required`           | `optional`, for scalars only        |

Two of these deserve a word.

An **open enumeration** pairs a fixed vocabulary with an escape hatch for values
the publisher did not foresee. Protobuf has no such type. The enum alone would
drop every extension value, so protoschemer adds a string field to carry them.

A **`"true"`/`"false"` enumeration** spells a boolean for a binding that lacks
one. Generating a two-member enum would force callers to compare against
constants, so protoschemer generates a real `bool`.

## Guarantees

- **The output compiles.** protoschemer builds a descriptor first and raises
  any problem as an error. It never writes text that only `protoc` will catch.
- **Imports follow references.** `--import-prefix` says where the output
  directory sits. protoschemer works out the rest.
- **Field numbers stay put.** Properties sort by name, so editing one property
  never renumbers another. Renumbering breaks the wire format.
- **Runs repeat exactly.** The same input yields byte-identical output.

## Flags

| Flag                    | Description                                          |
| ----------------------- | ---------------------------------------------------- |
| `-i`, `--in`            | Directory holding the schema documents (default `.`) |
| `-o`, `--out`           | Directory to write `.proto` files into (default `.`) |
| `-p`, `--proto-package` | Protobuf package for the generated files (required)  |
| `--go-package`          | Value for the generated `option go_package`          |
| `--import-prefix`       | Output directory relative to the protobuf root       |
| `--strip-suffix`        | Type-name suffix to drop, such as `DType`            |
| `--header`              | Comment placed at the top of every generated file    |
| `--name-map`            | JSON file naming each document's root message        |
| `-n`, `--dry-run`       | Print to stdout instead of writing files             |

Every flag also reads from a `PROTOSCHEMER_`-prefixed environment variable.

### About `--name-map`

A document's root schema becomes a message only when `--name-map` names it.
Without an entry, the document contributes only its definitions.

You supply the mapping because a file name often carries wire artifacts, such as
`svc-getallusers-200-responsepayload.json`. No rule recovers `GetAllUsers` from
`getallusers`, so protoschemer asks rather than guesses.

```json
{ "svc-getallusers-200-responsepayload": "GetAllUsersResponse" }
```

## Design

```text
cmd/jsonschema/       flags, file read and write
internal/protoemit/   IR to .proto source
internal/jsonschema/  JSON Schema to IR
internal/protoir/     domain types and errors
```

`protoir` imports nothing else here. `protoemit` and `jsonschema` sit side by
side, and neither imports the other. A new input dialect or output form touches
one package.

Libraries handle parsing and printing. `swaggest/jsonschema-go` reads the schema,
and `jhump/protoreflect` builds and prints the descriptor. This repository holds
the mapping between them.

## Development

```sh
go test ./...
golangci-lint run ./...
climax lint .
```

## License

MIT. See [LICENSE](LICENSE).
