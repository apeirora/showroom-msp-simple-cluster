import assert from "node:assert/strict";
import {
  configUrlFor,
  layoutGraph,
  loadProviderOptions,
  parseProviderData,
} from "../ui/public/ui-extensions/ord/renderer.js";

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
assert.deepEqual(
  layoutGraph(resources).map(({ name, level }) => [name, level]),
  [["workers", 1], ["cluster", 2], ["cp", 0]],
);

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
