import assert from "node:assert/strict";
import {
  configUrlFor,
  formatCount,
  loadProviderOptions,
  matchMarketplaceProvider,
  parseProviderData,
  providerIconUrl,
} from "../ui/public/ui-extensions/ord/renderer.js";

assert.equal(formatCount(1, "provider"), "1 provider");
assert.equal(formatCount(3, "dependency", "dependencies"), "3 dependencies");

const marketplaceProviders = [
  {
    name: "arsys",
    providerMetadata: { spec: { displayName: "Arsys Hosting" } },
  },
  {
    name: "shoots.dati.eu",
    providerMetadata: { spec: { displayName: "Dati Hosting" } },
  },
  {
    name: "sap.eu",
    providerMetadata: { spec: { displayName: "SAP Hosting" } },
  },
];
assert.equal(
  matchMarketplaceProvider("arsys-virtual", marketplaceProviders)?.name,
  "arsys",
);
assert.equal(
  matchMarketplaceProvider("dati", marketplaceProviders)?.name,
  "shoots.dati.eu",
);
assert.equal(
  matchMarketplaceProvider("sap-gardener-ironcore", marketplaceProviders)?.name,
  "sap.eu",
);
assert.equal(matchMarketplaceProvider("kind", marketplaceProviders), undefined);

assert.equal(
  providerIconUrl({
    providerMetadata: {
      spec: { icon: { light: { data: "data:image/png;base64,aGVsbG8=" } } },
    },
  }),
  "data:image/png;base64,aGVsbG8=",
);
assert.equal(
  providerIconUrl({
    providerMetadata: {
      spec: { icon: { light: { data: "javascript:alert(1)" } } },
    },
  }),
  undefined,
);

const provider = {
  providerMetadata: {
    spec: {
      data: JSON.stringify({
        ord: { configUrl: "https://simple-cluster.example/.well-known/open-resource-discovery" },
      }),
    },
  },
};

assert.deepEqual(parseProviderData("not-json"), {});
assert.equal(
  configUrlFor(provider),
  "https://simple-cluster.example/.well-known/open-resource-discovery",
);
assert.equal(
  configUrlFor({ providerMetadata: { spec: { data: { ord: { configUrl: "file:///etc/passwd" } } } } }),
  undefined,
);

const resources = [
  { name: "workers", apiVersion: "example.eu/v1", kind: "Machine", dependsOn: ["cp"] },
  { name: "cluster", apiVersion: "example.eu/v1", kind: "Cluster", dependsOn: ["cp", "workers"] },
  { name: "cp", apiVersion: "example.eu/v1", kind: "Machine", dependsOn: [] },
];
const responses = new Map([
  [
    "https://simple-cluster.example/.well-known/open-resource-discovery",
    {
      openResourceDiscoveryV1: {
        documents: [
          {
            url: "/ord/simple-cluster.json",
            accessStrategies: [{ type: "open" }],
          },
        ],
      },
    },
  ],
  [
    "https://simple-cluster.example/ord/simple-cluster.json",
    {
      openResourceDiscovery: "1.16",
      capabilities: [
        {
          type: "apeirora.eu:simple-cluster-provider-config:v1",
          title: "Example",
          definitions: [
            {
              mediaType: "application/json",
              url: "/ord/provider-config-example.json",
              accessStrategies: [{ type: "open" }],
            },
          ],
        },
      ],
    },
  ],
  [
    "https://simple-cluster.example/ord/provider-config-example.json",
    { name: "example", resources },
  ],
]);

const catalog = await loadProviderOptions(provider, async (url) => {
  assert.ok(responses.has(url), `unexpected fetch ${url}`);
  return responses.get(url);
});
assert.equal(catalog.options.length, 1);
assert.equal(catalog.options[0].graph.resources[1].dependsOn.length, 2);

const invalidProvider = structuredClone(provider);
await assert.rejects(
  loadProviderOptions(invalidProvider, async (url) => {
    if (url.endsWith("open-resource-discovery")) {
      return {
        openResourceDiscoveryV1: {
          documents: [{ url: "/bad", accessStrategies: [{ type: "open" }] }],
        },
      };
    }
    return { openResourceDiscovery: "2.0" };
  }),
  /Unsupported ORD version/,
);
