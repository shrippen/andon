#!/usr/bin/env python3
"""Derive the neutral sample world from the Studio Weber demo world.

Release builds (go build -tags release) carry no demo mode and no Studio
Weber data, but the gallery still previews tiles for services a user has
not connected. Those previews use sample.json: the same ids, numbers and
dates as world.json, with every name, address and note replaced by a
neutral placeholder ("Kunde A GmbH", "Projekt 1").

Run after sync-demo.py updated world.json:

    python3 demo/make-sample.py
"""
import json
import re
import string
from pathlib import Path

DIR = Path(__file__).resolve().parent.parent / "internal" / "sources" / "demoworld"
LETTERS = string.ascii_uppercase


# Sections the demo datasets read as they are, with Studio Weber's names
# swapped for the sample's (neutral() maps them; the world resolved its
# {{…}} references when it was built). The run fails if a name is left.
COPY = ["public_holidays", "monitoring", "server", "virtualization", "storage", "disk_health", "containers",
        "stacks", "backups", "certs", "domains", "mail_blacklist", "dns", "gateway", "vpn", "tailnet", "tunnel",
        "speed", "identity", "passwords", "cloud", "downloads", "code", "json_api", "smart_home", "pantry",
        "kitchen", "energy", "weather", "sites", "feeds", "bookmarks", "mail", "calendar", "photos", "library",
        "series", "bookkeeping", "documents", "assets_state", "bank", "subscriptions", "suggestions", "location",
        "logbook", "trips", "it_docs", "heartbeats", "prometheus", "vulnerabilities", "ci", "backup_server", "file_backups", "image_updates", "power", "proxy_routes", "dns_technitium", "fritzbox", "listening", "todo"]


def without_notes(node):
    """The value without its "note" keys (they tell the story by name)."""
    if isinstance(node, dict):
        return {k: without_notes(v) for k, v in node.items() if k != "note"}
    if isinstance(node, list):
        return [without_notes(v) for v in node]
    return node


def neutral(world, sample):
    """Studio Weber's names → the sample's, longest first (entries pair up by position)."""
    pairs = {world["studio"][k]: sample["studio"][k] for k in ("name", "domain", "city")}
    for kind, keys in (("people", ("name", "alias", "email")), ("customers", ("name",)), ("vendors", ("name", "domain"))):
        for a, b in zip(world[kind], sample[kind]):
            pairs.update({a[k]: b[k] for k in keys})
    for a, b in zip(world["projects"], sample["projects"]):
        pairs.update({a["name"][l]: b["name"][l] for l in ("de", "en")})
        pairs[a.get("short") or a["name"]["de"]] = b["short"]
    for a, b in zip(world["receipts"], sample["receipts"]):
        pairs[a["vendor"]] = b["vendor"]
    pairs[world["media"]["album"]["title"]] = sample["media"]["album"]["title"]
    pairs = {k: v for k, v in pairs.items() if k}
    return lambda text: re.sub("|".join(re.escape(k) for k in sorted(pairs, key=len, reverse=True)),
                               lambda m: pairs[m.group(0)], text)


def identity(world):
    """Names that tell Studio Weber apart (as scripts/release-check.sh lists them)."""
    names = {world["studio"]["name"], world["studio"]["domain"], world["studio"]["city"]}
    names |= {p["name"] for p in world["people"]} | {c["name"] for c in world["customers"]}
    names |= {v["name"] for v in world["vendors"]} | {r["vendor"] for r in world["receipts"]}
    names |= {p["short"] for p in world["projects"] if p.get("short")}
    names |= {t for p in world["projects"] for t in p["name"].values()}
    return {n for n in names if len(n) >= 5}


def text(de, en):
    return {"de": de, "en": en}


def main():
    world = json.loads((DIR / "world.json").read_text(encoding="utf-8"))
    vendor_names = {}

    def vendor(name):
        if name not in vendor_names:
            vendor_names[name] = f"Lieferant {len(vendor_names) + 1}"
        return vendor_names[name]

    sample = {
        "studio": {"name": "Beispielbüro", "hostname": "server", "domain": "example.test", "city": "Musterstadt"},
        "demo_password": "",
        "people": [
            {"id": p["id"], "name": f"Person {LETTERS[i]}", "alias": f"Person {LETTERS[i]}",
             "email": f"person-{LETTERS[i].lower()}@example.test"}
            for i, p in enumerate(world["people"])
        ],
        "customers": [
            {"id": c["id"], "name": f"Kunde {LETTERS[i]} GmbH", "country": c["country"],
             "vat_id": c["country"] + "0" * max(0, len(c.get("vat_id", "")) - 2) if c.get("vat_id") else ""}
            for i, c in enumerate(world["customers"])
        ],
        "projects": [
            {"id": p["id"], "customer": p["customer"], "color": p["color"], "hourly_rate": p["hourly_rate"],
             "name": text(f"Projekt {i + 1}", f"Project {i + 1}"), "short": f"Projekt {i + 1}"}
            for i, p in enumerate(world["projects"])
        ],
        "activities": [
            {"id": a["id"], "name": text(f"Tätigkeit {i + 1}", f"Activity {i + 1}")}
            for i, a in enumerate(world["activities"])
        ],
        "places": [
            {"id": p["id"], "name": text(f"Ort {i + 1}", f"Place {i + 1}"), "lat": p["lat"], "lon": p["lon"],
             **({"customer": p["customer"]} if p.get("customer") else {})}
            for i, p in enumerate(world["places"])
        ],
        "vendors": [
            {"id": v["id"], "name": vendor(v["name"]), "domain": f"lieferant-{i + 1}.example.test",
             "kind": v["kind"], "contract": v["contract"], "monthly": v["monthly"]}
            for i, v in enumerate(world["vendors"])
        ],
        "receipts": [
            {"id": r["id"], "vendor": vendor(r["vendor"]), "day": r["day"], "amount": r["amount"],
             "number": re.sub(r"^[A-Z]+", "RE", r["number"]), "category": r["category"],
             "note": text("", "")}
            for r in world["receipts"]
        ],
        "inventory": {
            "assets": [
                {"tag": a["tag"], "name": f"Gerät {i + 1}", "model": f"Modell {i + 1}",
                 "category": a["category"], "cost": a["cost"]}
                for i, a in enumerate(world["inventory"]["assets"])
            ],
            "licenses": [
                {"name": f"Lizenz {i + 1}", "vendor": l["vendor"], "seats": l["seats"]}
                for i, l in enumerate(world["inventory"]["licenses"])
            ],
            "disks": [f"Platte {i + 1}" for i in range(len(world["inventory"]["disks"]))],
        },
        "media": {"album": {"title": "Album", "artist": "Künstler"}},
    }
    copied = neutral(world, sample)(json.dumps({k: without_notes(world[k]) for k in COPY}, ensure_ascii=False))
    leaks = sorted(n for n in identity(world) if n in copied)
    if leaks:
        raise SystemExit(f"make-sample: {leaks} left in the copied sections; reference them with {{{{…}}}} in the world")
    sample.update(json.loads(copied))
    out = json.dumps(sample, ensure_ascii=False, indent=1) + "\n"
    (DIR / "sample.json").write_text(out, encoding="utf-8")


if __name__ == "__main__":
    main()
