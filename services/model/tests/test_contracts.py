from __future__ import annotations

import json
from pathlib import Path
import unittest

from services.model.vfl_contracts import (
    canonical_match_id,
    canonical_team_uid,
    canonical_tournament_id,
    parse_decimal_odds,
)


ROOT = Path(__file__).resolve().parents[3]
FIXTURES = ROOT / "fixtures" / "provider"


def load(name: str) -> dict:
    return json.loads((FIXTURES / name).read_text(encoding="utf-8"))


class ContractTests(unittest.TestCase):
    def test_canonical_namespaces(self) -> None:
        self.assertEqual(canonical_match_id("vf:match:1412174996"), 1412174996)
        self.assertEqual(canonical_team_uid("sr:competitor:276515"), 276515)
        self.assertEqual(canonical_tournament_id("vf:tournament:14149"), 14149)

    def test_decimal_odds_are_strings_and_zero_is_rejected(self) -> None:
        self.assertEqual(parse_decimal_odds("1.75"), "1.75")
        with self.assertRaises(ValueError):
            parse_decimal_odds("0.00")

    def test_fixture_sample_shape(self) -> None:
        completed = load("completed-season-fixtures.json")
        current = load("upcoming-matchdays.json")
        self.assertEqual(len(completed["doc"][0]["data"]["matches"]), 240)
        matches = current["doc"][0]["data"]["matches"]
        self.assertEqual(len(matches), 240)
        self.assertEqual(sum(m["result"]["home"] is None for m in matches), 80)
        self.assertEqual(sum(isinstance(m["periods"], list) for m in matches), 80)

    def test_odds_sample_shape_and_raw_decimal_values(self) -> None:
        odds = load("historical-match-odds.json")["doc"][0]["data"]["odds"]
        self.assertEqual(len(odds), 8)
        self.assertTrue(all(isinstance(o["odds"], str) for r in odds for m in r["market"] for o in m["outcome"]))
        self.assertEqual(
            sum(o["odds"] == "0.00" for r in odds for m in r["market"] for o in m["outcome"]),
            1,
        )

    def test_all_supplied_envelopes_have_observed_query_urls(self) -> None:
        expected = {
            "historical-seasons.json": "uniquetournament_seasons/14149",
            "completed-season-fixtures.json": "stats_season_fixtures2/3149311",
            "current-season-meta.json": "stats_season_meta/3149369",
            "upcoming-matchdays.json": "stats_season_fixtures2/3149369",
            "historical-match-odds.json": "stats_season_odds2/vf:season:3149336/23",
            "team-position-history.json": "stats_season_teampositionhistory/3149311/276510",
            "live-matchday-status.json": "vfl_tournament_livetablebyseasonandround/3149369/28",
        }
        for name, query_url in expected.items():
            with self.subTest(name=name):
                self.assertEqual(load(name)["queryUrl"], query_url)


if __name__ == "__main__":
    unittest.main()
