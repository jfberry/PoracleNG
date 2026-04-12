#!/usr/bin/env python3
"""Search webhook logs for pokemon and optionally replay them.

Usage:
    python3 tools/webhook-search.py [options]

Search examples:
    # Find by pokemon name/id and IVs
    python3 tools/webhook-search.py --pokemon poliwag --atk 0 --def 15 --sta 13
    python3 tools/webhook-search.py --pokemon 60 --cp 53

    # Find any pokemon with PVP evolution data
    python3 tools/webhook-search.py --has-evo --limit 5

    # Find specific evolution in PVP data
    python3 tools/webhook-search.py --evo-pokemon 186  # Politoed in PVP data

    # Find by IV percentage / hundos
    python3 tools/webhook-search.py --pokemon poliwag --iv 62.22
    python3 tools/webhook-search.py --iv 100

    # Show raw webhook JSON
    python3 tools/webhook-search.py --pokemon poliwag --raw

Replay examples:
    # Replay first match into local processor
    python3 tools/webhook-search.py --pokemon poliwag --has-evo --replay

    # Replay all matches (adjusts disappear_time to 30min from now)
    python3 tools/webhook-search.py --has-evo --limit 5 --replay --replay-all

    # Replay to specific endpoint with custom TTH
    python3 tools/webhook-search.py --pokemon 60 --replay --replay-url http://host:3030 --replay-tth 60

    # Replay with adjusted location
    python3 tools/webhook-search.py --pokemon 60 --replay --replay-lat 51.28 --replay-lon 1.08

    # Dry run — show what would be sent
    python3 tools/webhook-search.py --pokemon 60 --replay --dry-run

    # Inject raw webhook JSON directly (string or @file)
    python3 tools/webhook-search.py --inject '{"type":"pokemon","message":{...}}'
    python3 tools/webhook-search.py --inject @/path/to/webhook.json
    python3 tools/webhook-search.py --inject @webhook.json --dry-run

    # Search non-pokemon webhooks
    python3 tools/webhook-search.py --type raid --limit 5 --raw
    python3 tools/webhook-search.py --type raid --replay
"""

import argparse
import gzip
import glob
import json
import os
import sys
import time
import urllib.request

POKEMON_NAMES = {}
POKEMON_ID_TO_NAME = {}
BASE_DIR = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))


def get_processor_url():
    """Read processor host/port from config.toml."""
    config_path = os.path.join(BASE_DIR, "config", "config.toml")
    host = "localhost"
    port = 3030
    if not os.path.exists(config_path):
        return f"http://{host}:{port}"
    try:
        with open(config_path) as f:
            in_processor = False
            for line in f:
                stripped = line.strip()
                if stripped == "[processor]":
                    in_processor = True
                    continue
                if stripped.startswith("[") and in_processor:
                    in_processor = False
                    continue
                if in_processor:
                    if stripped.startswith("host"):
                        val = stripped.split("=", 1)[1].strip().strip('"').strip("'")
                        if val:
                            host = val
                    elif stripped.startswith("port"):
                        val = stripped.split("=", 1)[1].strip().strip('"').strip("'")
                        try:
                            port = int(val)
                        except ValueError:
                            pass
    except Exception:
        pass
    # 0.0.0.0 isn't useful as a connect target
    if host == "0.0.0.0":
        host = "localhost"
    return f"http://{host}:{port}"


def load_pokemon_names():
    """Load pokemon names from resources/rawdata/pokemon.json."""
    global POKEMON_NAMES, POKEMON_ID_TO_NAME
    if POKEMON_NAMES:
        return
    base = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
    rawdata = os.path.join(base, "resources", "rawdata", "pokemon.json")
    if not os.path.exists(rawdata):
        return
    try:
        with open(rawdata) as f:
            data = json.load(f)
        for pid_str, info in data.items():
            if isinstance(info, dict):
                name = info.get("pokemonName") or info.get("name") or ""
                if name:
                    pid = int(pid_str)
                    POKEMON_NAMES[name.lower()] = pid
                    POKEMON_ID_TO_NAME[pid] = name
    except Exception:
        pass


def pokemon_name(pid):
    """Get pokemon name from ID."""
    load_pokemon_names()
    return POKEMON_ID_TO_NAME.get(pid, f"Pokemon#{pid}")


def resolve_pokemon_id(name_or_id):
    """Resolve a pokemon name or ID string to an integer ID."""
    try:
        return int(name_or_id)
    except ValueError:
        load_pokemon_names()
        return POKEMON_NAMES.get(name_or_id.lower())


def calc_iv(atk, def_, sta):
    if atk is None or def_ is None or sta is None:
        return -1
    return round((atk + def_ + sta) / 45 * 100, 2)


def find_log_files(log_path):
    """Find the main log file and all rotated .gz files, sorted oldest first."""
    log_dir = os.path.dirname(log_path)
    base_name = os.path.basename(log_path)
    stem = os.path.splitext(base_name)[0]

    # Find .gz rotated files
    gz_pattern = os.path.join(log_dir, f"{stem}-*.log.gz")
    gz_files = sorted(glob.glob(gz_pattern))

    # Also check for uncompressed rotated files
    rot_pattern = os.path.join(log_dir, f"{stem}-*.log")
    rot_files = sorted(f for f in glob.glob(rot_pattern) if not f.endswith(".gz"))

    # Order: old gz files, old uncompressed, current log
    files = gz_files + rot_files
    if os.path.exists(log_path):
        files.append(log_path)

    return files


def open_log(path):
    """Open a log file, handling .gz transparently."""
    if path.endswith(".gz"):
        return gzip.open(path, "rt", errors="replace")
    return open(path, "r", errors="replace")


def matches_filters(obj, msg, args, pokemon_id):
    """Check if a webhook message matches all filters. Returns (match, pvp, has_evo)."""
    webhook_type = obj.get("type", "")

    # Type filter — default to pokemon unless --type specified
    wanted_type = args.type or "pokemon"
    if webhook_type != wanted_type:
        return False, None, False

    # Type-specific filters for non-pokemon types
    if webhook_type == "raid":
        if args.raid_pokemon is not None:
            raid_pid = resolve_pokemon_id(args.raid_pokemon)
            if raid_pid is None:
                return False, None, False
            if msg.get("pokemon_id") != raid_pid:
                return False, None, False
        if args.raid_level is not None and msg.get("level") != args.raid_level:
            return False, None, False
        if args.egg and msg.get("pokemon_id", 0) != 0:
            return False, None, False
        if args.hatched and msg.get("pokemon_id", 0) == 0:
            return False, None, False
        return True, None, False

    if webhook_type == "pokestop":
        if args.lure_type is not None and msg.get("lure_id") != args.lure_type:
            return False, None, False
        return True, None, False

    if webhook_type == "quest":
        if args.reward is not None:
            # Search across reward fields
            reward_str = json.dumps(msg.get("rewards", [])).lower()
            quest_str = json.dumps(msg).lower()
            if args.reward.lower() not in reward_str and args.reward.lower() not in quest_str:
                return False, None, False
        return True, None, False

    if webhook_type == "max_battle":
        if args.max_pokemon is not None:
            max_pid = resolve_pokemon_id(args.max_pokemon)
            if max_pid is None:
                return False, None, False
            if msg.get("pokemon_id") != max_pid:
                return False, None, False
        return True, None, False

    # For other non-pokemon types, no pokemon-specific filters
    if webhook_type != "pokemon":
        return True, None, False

    # Pokemon ID
    if pokemon_id is not None and msg.get("pokemon_id") != pokemon_id:
        return False, None, False

    # IVs
    if args.atk is not None and msg.get("individual_attack") != args.atk:
        return False, None, False
    if args.def_ is not None and msg.get("individual_defense") != args.def_:
        return False, None, False
    if args.sta is not None and msg.get("individual_stamina") != args.sta:
        return False, None, False

    # CP / Level
    if args.cp is not None and msg.get("cp") != args.cp:
        return False, None, False
    if args.level is not None and msg.get("pokemon_level") != args.level:
        return False, None, False

    # IV percentage
    if args.iv is not None:
        actual_iv = calc_iv(
            msg.get("individual_attack"),
            msg.get("individual_defense"),
            msg.get("individual_stamina"),
        )
        if abs(actual_iv - args.iv) > 0.05:
            return False, None, False

    # PVP evolution checks
    pvp = msg.get("pvp")
    has_evo = False
    if pvp and isinstance(pvp, dict):
        pid = msg.get("pokemon_id")
        for league, entries in pvp.items():
            if not entries:
                continue
            for e in entries:
                ep = e.get("pokemon", 0)
                if ep != pid and ep > 0:
                    has_evo = True
                    break
            if has_evo:
                break

    if args.has_evo and not has_evo:
        return False, None, False

    # PVP rank filter
    if args.pvp_rank is not None:
        found_rank = False
        if pvp and isinstance(pvp, dict):
            leagues_to_check = [args.pvp_league] if args.pvp_league else list(pvp.keys())
            for league in leagues_to_check:
                entries = pvp.get(league)
                if not entries:
                    continue
                for e in entries:
                    rank = e.get("rank", 9999)
                    if rank <= args.pvp_rank:
                        found_rank = True
                        break
                if found_rank:
                    break
        if not found_rank:
            return False, None, False

    if args.evo_pokemon is not None:
        found_evo = False
        if pvp and isinstance(pvp, dict):
            for league, entries in pvp.items():
                if not entries:
                    continue
                for e in entries:
                    if e.get("pokemon") == args.evo_pokemon:
                        found_evo = True
                        break
                if found_evo:
                    break
        if not found_evo:
            return False, None, False

    return True, pvp, has_evo


def display_result(idx, timestamp, msg, pvp, has_evo, args):
    """Display a single search result."""
    load_pokemon_names()
    pid = msg.get("pokemon_id", 0)
    name = pokemon_name(pid)
    iv = calc_iv(
        msg.get("individual_attack"),
        msg.get("individual_defense"),
        msg.get("individual_stamina"),
    )
    cp = msg.get("cp", "?")
    level = msg.get("pokemon_level", "?")
    atk = msg.get("individual_attack", "?")
    def_ = msg.get("individual_defense", "?")
    sta = msg.get("individual_stamina", "?")
    form = msg.get("form", 0)
    gender = msg.get("gender", 0)
    lat = msg.get("latitude", 0)
    lon = msg.get("longitude", 0)

    ts_str = f" [{timestamp}]" if timestamp else ""
    print(f"\n{'='*60}")

    # Raid/egg-specific header
    gym_name = msg.get("gym_name", "")
    raid_level = msg.get("level")
    if gym_name and raid_level is not None:
        if pid == 0:
            print(f"[{idx}] Egg L{raid_level} at {gym_name}{ts_str}")
        else:
            print(f"[{idx}] Raid L{raid_level} {name} (#{pid}) at {gym_name}{ts_str}")
    else:
        print(f"[{idx}] {name} (#{pid}) form:{form}{ts_str}")

    if iv >= 0:
        print(f"  IV: {iv}% ({atk}/{def_}/{sta})  CP: {cp}  L{level}  Gender: {gender}")
    elif cp and cp != "?" and cp != 0:
        print(f"  CP: {cp}  Gender: {gender}")
    print(f"  Location: {lat:.6f}, {lon:.6f}")

    if pvp and isinstance(pvp, dict):
        for league, entries in sorted(pvp.items()):
            if not entries:
                continue
            league_label = {"great": "GL", "ultra": "UL", "little": "LL"}.get(league, league)
            print(f"  PVP {league_label}:")
            for e in entries:
                ep = e.get("pokemon", pid)
                evo_marker = " [EVO]" if ep != pid else ""
                evo_name = pokemon_name(ep) if ep != pid else ""
                form_str = f" form:{e.get('form', 0)}" if e.get('form', 0) else ""
                print(
                    f"    #{e.get('rank', '?'):>4}  {evo_name or name}{form_str}"
                    f"  CP:{e.get('cp', '?')}  L{e.get('level', '?')}"
                    f"  {e.get('percentage', 0) * 100:.1f}%{evo_marker}"
                )

    if args.raw:
        print(f"  RAW: {json.dumps(msg)}")


def replay_webhook(msg, args, webhook_type="pokemon"):
    """POST a webhook back to the processor with adjusted timestamps."""
    url = (args.replay_url or get_processor_url()).rstrip("/") + "/"
    tth_seconds = (args.replay_tth or 30) * 60

    # Adjust timestamps to future
    now = int(time.time())
    msg = dict(msg)  # copy
    if "disappear_time" in msg:
        msg["disappear_time"] = now + tth_seconds
    if "first_seen" in msg:
        msg["first_seen"] = now - 60
    if "last_modified_time" in msg:
        msg["last_modified_time"] = now
    # Raid timestamps
    if "end" in msg and webhook_type == "raid":
        msg["end"] = now + tth_seconds
        msg["start"] = now - 300
        msg["spawn"] = now - 600
    # Invasion/lure expiry
    if "incident_expire_timestamp" in msg:
        msg["incident_expire_timestamp"] = now + tth_seconds
    if "lure_expiration" in msg:
        msg["lure_expiration"] = now + tth_seconds

    # Adjust location if requested
    if args.replay_lat is not None:
        msg["latitude"] = args.replay_lat
    if args.replay_lon is not None:
        msg["longitude"] = args.replay_lon

    payload = [{"type": webhook_type, "message": msg}]
    data = json.dumps(payload).encode()

    name = pokemon_name(msg.get("pokemon_id", 0))
    iv = calc_iv(
        msg.get("individual_attack"),
        msg.get("individual_defense"),
        msg.get("individual_stamina"),
    )

    if args.dry_run:
        print(f"  [DRY RUN] Would POST {name} {iv}% to {url}")
        print(f"  disappear_time={msg['disappear_time']} ({tth_seconds // 60}m from now)")
        if args.replay_lat is not None or args.replay_lon is not None:
            print(f"  location={msg['latitude']},{msg['longitude']}")
        return True

    try:
        req = urllib.request.Request(
            url,
            data=data,
            headers={"Content-Type": "application/json"},
            method="POST",
        )
        with urllib.request.urlopen(req, timeout=10) as resp:
            status = resp.status
        print(f"  -> Replayed {name} {iv}% to {url} (HTTP {status})")
        return True
    except Exception as e:
        print(f"  -> FAILED to replay: {e}", file=sys.stderr)
        return False


def inject_webhook(args):
    """Replay a webhook from raw JSON (string or @filename)."""
    raw = args.inject
    if raw.startswith("@"):
        path = raw[1:]
        if not os.path.exists(path):
            print(f"File not found: {path}", file=sys.stderr)
            sys.exit(1)
        with open(path) as f:
            raw = f.read().strip()

    try:
        data = json.loads(raw)
    except json.JSONDecodeError as e:
        print(f"Invalid JSON: {e}", file=sys.stderr)
        sys.exit(1)

    # Accept either {"type":"pokemon","message":{...}} or just the message {...}
    webhook_type = "pokemon"
    if isinstance(data, dict) and "type" in data and "message" in data:
        webhook_type = data["type"]
        msg = data["message"]
    elif isinstance(data, dict):
        msg = data
        # Guess type from fields
        if "pokemon_id" in msg and "gym_id" not in msg:
            webhook_type = "pokemon"
        elif "gym_id" in msg and "level" in msg:
            webhook_type = "raid"
        elif "pokestop_id" in msg and "quest_type" in msg:
            webhook_type = "quest"
        elif "incident_expire_timestamp" in msg:
            webhook_type = "invasion"
        elif "lure_id" in msg:
            webhook_type = "pokestop"
    else:
        print("JSON must be a webhook object or a message object", file=sys.stderr)
        sys.exit(1)

    load_pokemon_names()
    pvp = msg.get("pvp")
    has_evo = False
    if pvp and isinstance(pvp, dict):
        pid = msg.get("pokemon_id")
        for entries in pvp.values():
            if entries:
                for e in entries:
                    if e.get("pokemon", 0) != pid and e.get("pokemon", 0) > 0:
                        has_evo = True
                        break

    display_result(1, "inject", msg, pvp, has_evo, args)
    print()
    replay_webhook(msg, args, webhook_type=webhook_type)


def search_and_display(args):
    """Main search logic."""
    # Resolve log path
    log_path = args.log
    if not log_path:
        base = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
        log_path = os.path.join(base, "logs", "webhooks.log")

    # Resolve pokemon name
    pokemon_id = None
    if args.pokemon:
        pokemon_id = resolve_pokemon_id(args.pokemon)
        if pokemon_id is None:
            print(f"Unknown pokemon: {args.pokemon}", file=sys.stderr)
            sys.exit(1)

    # Find all log files (including .gz history)
    log_files = find_log_files(log_path)
    if not log_files:
        print(f"No log files found at {log_path}", file=sys.stderr)
        sys.exit(1)

    results = []
    total_lines = 0

    for log_file in log_files:
        try:
            with open_log(log_file) as f:
                for line in f:
                    line = line.strip()
                    if not line:
                        continue
                    total_lines += 1

                    # Strip timestamp prefix
                    timestamp = ""
                    if line[0] != "{":
                        idx = line.find("{")
                        if idx < 0:
                            continue
                        timestamp = line[:idx].strip()
                        line = line[idx:]

                    try:
                        obj = json.loads(line)
                    except json.JSONDecodeError:
                        continue

                    msg = obj.get("message", {})
                    match, pvp, has_evo = matches_filters(obj, msg, args, pokemon_id)
                    if not match:
                        continue

                    source = os.path.basename(log_file)
                    results.append((timestamp, msg, pvp, has_evo, source))

                    if args.limit and len(results) >= args.limit:
                        break
        except Exception as e:
            print(f"Warning: could not read {log_file}: {e}", file=sys.stderr)
            continue

        if args.limit and len(results) >= args.limit:
            break

    # Display
    if not results:
        files_str = ", ".join(os.path.basename(f) for f in log_files)
        print(f"No matches found ({total_lines} lines across {len(log_files)} file(s): {files_str})")
        return

    load_pokemon_names()

    for i, (timestamp, msg, pvp, has_evo, source) in enumerate(results):
        ts_display = timestamp or source
        display_result(i + 1, ts_display, msg, pvp, has_evo, args)

    print(f"\n{'='*60}")
    print(f"Found {len(results)} match(es) in {total_lines} lines across {len(log_files)} file(s)")

    # Replay
    webhook_type = args.type or "pokemon"
    if args.replay:
        print()
        if args.replay_all:
            for i, (_, msg, _, _, _) in enumerate(results):
                print(f"Replaying [{i + 1}/{len(results)}]...")
                replay_webhook(msg, args, webhook_type=webhook_type)
        else:
            # Replay first match only
            print("Replaying match [1]...")
            replay_webhook(results[0][1], args, webhook_type=webhook_type)

        if not args.dry_run:
            print(f"\nReplayed {'all ' + str(len(results)) if args.replay_all else '1'} webhook(s)")


def main():
    parser = argparse.ArgumentParser(
        description="Search webhook logs for pokemon and optionally replay them",
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )

    search = parser.add_argument_group("Search filters")
    search.add_argument("--pokemon", "-p", help="Pokemon name or ID")
    search.add_argument("--atk", type=int, help="Attack IV")
    search.add_argument("--def", dest="def_", type=int, help="Defense IV")
    search.add_argument("--sta", type=int, help="Stamina IV")
    search.add_argument("--cp", type=int, help="CP")
    search.add_argument("--level", "-l", type=int, help="Level")
    search.add_argument("--iv", type=float, help="IV percentage (e.g. 62.22)")
    search.add_argument("--has-evo", action="store_true", help="Only pokemon with PVP evolution entries")
    search.add_argument("--evo-pokemon", type=int, help="Filter by evolution pokemon ID in PVP data")
    search.add_argument("--pvp-rank", type=int, help="Max PVP rank to match (e.g. 10 for top 10)")
    search.add_argument("--pvp-league", help="PVP league filter: great, ultra, little (default: any)")
    search.add_argument("--type", "-t", help="Webhook type to search (default: pokemon). Use 'raid', 'quest', 'invasion', 'pokestop', 'max_battle', etc.")

    typed = parser.add_argument_group("Type-specific filters")
    typed.add_argument("--raid-pokemon", help="Raid boss pokemon name or ID (hatched raids only)")
    typed.add_argument("--raid-level", type=int, help="Raid level")
    typed.add_argument("--egg", action="store_true", help="Only eggs (pokemon_id=0)")
    typed.add_argument("--hatched", action="store_true", help="Only hatched raids (pokemon_id>0)")
    typed.add_argument("--lure-type", type=int, help="Lure type ID (501=normal, 502=glacial, 503=mossy, 504=magnetic, 505=rainy, 506=sparkly)")
    typed.add_argument("--reward", help="Quest reward text search (substring match)")
    typed.add_argument("--max-pokemon", help="Max battle pokemon name or ID")

    display = parser.add_argument_group("Display")
    display.add_argument("--raw", action="store_true", help="Show raw webhook JSON")
    display.add_argument("--limit", "-n", type=int, default=10, help="Max results (default 10)")
    display.add_argument("--log", help="Path to webhook log file (searches .gz history too)")

    replay_grp = parser.add_argument_group("Replay")
    replay_grp.add_argument("--replay", action="store_true", help="Replay first match to processor")
    replay_grp.add_argument("--replay-all", action="store_true", help="Replay all matches")
    replay_grp.add_argument("--replay-url", help="Processor URL (default: http://localhost:3030)")
    replay_grp.add_argument("--replay-tth", type=int, default=30, help="TTH in minutes for replayed webhooks (default: 30)")
    replay_grp.add_argument("--replay-lat", type=float, help="Override latitude for replay")
    replay_grp.add_argument("--replay-lon", type=float, help="Override longitude for replay")
    replay_grp.add_argument("--dry-run", action="store_true", help="Show what would be replayed without sending")
    replay_grp.add_argument("--inject", help="Replay raw webhook JSON (string or @filename) directly without searching logs")

    args = parser.parse_args()

    # Direct inject mode — skip search entirely
    if args.inject:
        args.replay = True
        inject_webhook(args)
        return

    if args.replay_all:
        args.replay = True
    search_and_display(args)


if __name__ == "__main__":
    main()
