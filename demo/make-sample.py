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
    out = json.dumps(sample, ensure_ascii=False, indent=1) + "\n"
    (DIR / "sample.json").write_text(out, encoding="utf-8")


if __name__ == "__main__":
    main()
