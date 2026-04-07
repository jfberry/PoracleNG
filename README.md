# PoracleNG Images

Static images served via raw GitHub URLs for use in PoracleNG fallback templates and DTS rendering.

This is an **orphan branch** — it does not share history with the main code branches.

## Usage

Reference these directly via raw URLs in your `config.toml`:

```toml
[fallbacks]
static_map = "https://raw.githubusercontent.com/jfberry/PoracleNG/images/fallback/staticMap.png"
img_url = "https://raw.githubusercontent.com/jfberry/PoracleNG/images/fallback/mon.png"
img_url_weather = "https://raw.githubusercontent.com/jfberry/PoracleNG/images/fallback/weather.png"
img_url_egg = "https://raw.githubusercontent.com/jfberry/PoracleNG/images/fallback/uni.png"
img_url_gym = "https://raw.githubusercontent.com/jfberry/PoracleNG/images/fallback/gym.png"
img_url_pokestop = "https://raw.githubusercontent.com/jfberry/PoracleNG/images/fallback/pokestop.png"
pokestop_url = "https://raw.githubusercontent.com/jfberry/PoracleNG/images/fallback/pokestop.png"
```

## Contents

- `fallback/` — fallback images used when the normal pokemon/gym/raid icon URLs fail
- `starchy.svg` — bot logo

## Origin

These were copied from [`KartulUdus/PoracleJS:images`](https://github.com/KartulUdus/PoracleJS/tree/images) at the time PoracleNG forked.
