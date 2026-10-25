# Supplied provider fixtures

These seven `.json` files are verbatim copies of the owner-supplied `.txt` JSON payloads. They are golden test inputs, not a live cache or a credential store.

| Golden file | Supplied source | Observed query URL |
|---|---|---|
| `historical-seasons.json` | `Historical seasons list endpoint JSON response.txt` | `uniquetournament_seasons/14149` |
| `completed-season-fixtures.json` | `Completed season results endpoint JSON response.txt` | `stats_season_fixtures2/3149311` |
| `current-season-meta.json` | `Current season information endpoint JSON response.txt` | `stats_season_meta/3149369` |
| `upcoming-matchdays.json` | `Upcoming matchdays endpoint JSON response.txt` | `stats_season_fixtures2/3149369` |
| `historical-match-odds.json` | `Historical match odds endpoint JSON response.txt` | `stats_season_odds2/vf:season:3149336/23` |
| `team-position-history.json` | `Historical team statistics position history endpoint JSON response.txt` | `stats_season_teampositionhistory/3149311/276510` |
| `live-matchday-status.json` | `Live matchday status endpoint JSON response.txt` | `vfl_tournament_livetablebyseasonandround/3149369/28` |

The odds fixture intentionally does not join either supplied fixture fixture set. That discrepancy is asserted by a golden test and documented in `docs/api-assumptions.md`.
