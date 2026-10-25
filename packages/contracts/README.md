# Contract package

This directory contains the Phase 0 raw-provider JSON Schemas and the future internal OpenAPI boundary. The provider schemas describe the supplied payload shapes with forward-compatible unknown fields; semantic normalization and rejection rules live in the Go parser.

The schemas are evidence-based and unvalidated against a live service. They do not authorize requests, encode credentials, or claim that the documented URL templates are reachable.
