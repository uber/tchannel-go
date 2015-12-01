include "shared.thrift"

service Foo extends shared.FooBase {
  shared.UUID getMyUUID(1: shared.UUID uuid, 2: shared.Health health)
  shared.Health health(1: shared.UUID uuid, 2: shared.Health health)
}
