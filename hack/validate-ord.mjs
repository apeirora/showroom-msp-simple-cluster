import fs from "node:fs";
import path from "node:path";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";

const require = createRequire(new URL("../ui/package.json", import.meta.url));
const Ajv = require("ajv").default;
const addFormats = require("ajv-formats").default;
const {
  ordConfigurationSchema,
  ordDocumentSchema,
} = require("@open-resource-discovery/specification");

const here = path.dirname(fileURLToPath(import.meta.url));
const filename = path.join(
  here,
  "../simple-cluster-operator/internal/controller/testdata/ord-document.json",
);
const document = JSON.parse(fs.readFileSync(filename, "utf8"));
const ajv = new Ajv({ allErrors: true, strict: false });
addFormats(ajv);
const validate = ajv.compile(ordDocumentSchema);
if (!validate(document)) {
  throw new Error(`ORD 1.16.3 validation failed: ${ajv.errorsText(validate.errors)}`);
}

const configuration = JSON.parse(
  fs.readFileSync(path.join(path.dirname(filename), "ord-configuration.json"), "utf8"),
);
const validateConfiguration = ajv.compile(ordConfigurationSchema);
if (!validateConfiguration(configuration)) {
  throw new Error(
    `ORD configuration validation failed: ${ajv.errorsText(validateConfiguration.errors)}`,
  );
}
