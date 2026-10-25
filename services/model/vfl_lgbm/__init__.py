"""Phase 3.5 LightGBM research pipeline.

The package consumes the versioned dataset exported by the canonical Go
research loader. It is research-only: it never promotes a model or places a
bet.
"""

from .pipeline import Phase35Config, run_phase35

__all__ = ["Phase35Config", "run_phase35"]
