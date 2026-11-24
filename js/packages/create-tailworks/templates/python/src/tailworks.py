"""The caller's tailnet identity in a tailworks Python Worker.

tailworks checks the caller before your code runs: request.cf.tailscale.caller
is there only when hostd's seal over it verified (docs/python.md).
"""


def get_caller(request):
    """The verified tailnet caller as a dict with getCaller's fields
    (nodeId, tags, hostname, node, addrs, user {"login"} or None, capMap,
    objectId, traceId), or None: a public (Funnel) request, or a request
    tailworks didn't deliver."""
    cf = getattr(request.js_object, "cf", None)
    ts = getattr(cf, "tailscale", None) if cf else None
    if not ts:
        return None
    requester = getattr(ts, "requester", None)
    if requester and getattr(requester, "kind", None) == "public":
        return None
    caller = getattr(ts, "caller", None)
    if not caller:
        return None
    c = caller.to_py()
    if not c.get("nodeId"):
        return None
    # The same shape as getCaller in @tailworks/tailnet.
    return {
        "nodeId": c["nodeId"],
        "tags": list(c.get("tags") or []),
        "hostname": c.get("hostname", ""),
        "node": c.get("node", ""),
        "addrs": list(c.get("addrs") or []),
        "user": {"login": c["user"]} if c.get("user") else None,
        "capMap": c.get("capMap") or {},
        "objectId": getattr(cf, "objectId", None) or "",
        "traceId": getattr(cf, "traceId", None) or "",
    }


def caller_name(caller):
    """A display name for a caller: the user's login, else the hostname."""
    if not caller:
        return "anonymous"
    user = caller.get("user") or {}
    return user.get("login") or caller.get("hostname") or caller.get("nodeId")
