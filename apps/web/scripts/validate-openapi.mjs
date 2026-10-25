import path from "node:path";
import { fileURLToPath } from "node:url";
import SwaggerParser from "@apidevtools/swagger-parser";

const here = path.dirname(fileURLToPath(import.meta.url));
const specificationPath = path.resolve(here, "../../..", "packages/contracts/openapi.yaml");

await SwaggerParser.validate(specificationPath);
console.log(`OpenAPI specification is valid: ${specificationPath}`);
