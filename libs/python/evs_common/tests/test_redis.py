from evs_common.redis import TenantRedis


def test_key_namespacing():
    class FakeRedis:
        pass

    r = TenantRedis(FakeRedis(), "tenant-abc")
    assert r._key("session") == "t:tenant-abc:session"
