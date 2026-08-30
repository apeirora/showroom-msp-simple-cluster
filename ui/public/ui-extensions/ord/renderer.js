const PROTOCOL = "platform-mesh.provider-details.v1";
const RESIZE = "platform-mesh.provider-details.resize.v1";
const CAPABILITY_TYPE = "apeirora.eu:simple-cluster-provider-config:v1";
const MAX_DOCUMENTS = 10;
const MAX_BYTES = 2 * 1024 * 1024;

export function parseProviderData(data) {
  if (!data) return {};
  if (typeof data === "object") return data;
  if (typeof data !== "string") return {};
  try {
    const parsed = JSON.parse(data);
    return parsed && typeof parsed === "object" ? parsed : {};
  } catch {
    return {};
  }
}

export function configUrlFor(provider) {
  const data = parseProviderData(provider?.providerMetadata?.spec?.data);
  const value = data?.ord?.configUrl;
  if (typeof value !== "string") return undefined;
  try {
    const url = new URL(value);
    return ["http:", "https:"].includes(url.protocol) ? url.href : undefined;
  } catch {
    return undefined;
  }
}

export async function loadProviderOptions(provider, fetchJSON = fetchJson) {
  const configUrl = configUrlFor(provider);
  if (!configUrl) throw new Error("No ORD configuration URL");
  const configuration = await fetchJSON(configUrl, 256 * 1024);
  const declarations = configuration?.openResourceDiscoveryV1?.documents;
  if (!Array.isArray(declarations)) throw new Error("Invalid ORD configuration");

  const documents = await Promise.all(
    declarations
      .filter((item) =>
        (item.accessStrategies ?? []).some((strategy) => strategy.type === "open"),
      )
      .slice(0, MAX_DOCUMENTS)
      .map(async (item) => {
        const document = await fetchJSON(new URL(item.url, configUrl).href, MAX_BYTES);
        if (document?.openResourceDiscovery !== "1.16") {
          throw new Error("Unsupported ORD version");
        }
        return document;
      }),
  );

  const capabilities = documents.flatMap((document) =>
    (document.capabilities ?? []).filter((item) => item.type === CAPABILITY_TYPE),
  );
  const loaded = await Promise.allSettled(
    capabilities.map(async (capability) => {
      const definition = capability.definitions?.find(
        (item) =>
          item.mediaType === "application/json" &&
          (item.accessStrategies ?? []).some((strategy) => strategy.type === "open"),
      );
      if (!definition) throw new Error("Provider graph definition is missing");
      const graph = await fetchJSON(new URL(definition.url, configUrl).href, MAX_BYTES);
      validateGraph(graph);
      return { capability, graph };
    }),
  );
  return {
    configUrl,
    options: loaded
      .filter((result) => result.status === "fulfilled")
      .map((result) => result.value)
      .sort((left, right) => left.capability.title.localeCompare(right.capability.title)),
  };
}

export function layoutGraph(resources) {
  const byName = new Map(resources.map((resource) => [resource.name, resource]));
  const memo = new Map();
  const visiting = new Set();
  function level(name) {
    if (memo.has(name)) return memo.get(name);
    if (visiting.has(name)) return 0;
    visiting.add(name);
    const resource = byName.get(name);
    const dependencies = (resource?.dependsOn ?? []).filter((dependency) => byName.has(dependency));
    const value = dependencies.length
      ? 1 + Math.max(...dependencies.map((dependency) => level(dependency)))
      : 0;
    visiting.delete(name);
    memo.set(name, value);
    return value;
  }
  return resources.map((resource) => ({ ...resource, level: level(resource.name) }));
}

function validateGraph(graph) {
  if (!graph || typeof graph.name !== "string" || !Array.isArray(graph.resources)) {
    throw new Error("Invalid provider graph");
  }
  const names = new Set(graph.resources.map((resource) => resource.name));
  for (const resource of graph.resources) {
    if (
      typeof resource.name !== "string" ||
      typeof resource.apiVersion !== "string" ||
      typeof resource.kind !== "string" ||
      !Array.isArray(resource.dependsOn) ||
      resource.dependsOn.some((dependency) => !names.has(dependency))
    ) {
      throw new Error("Invalid provider graph node");
    }
  }
}

async function fetchJson(url, maxBytes) {
  const controller = new AbortController();
  const timeout = window.setTimeout(() => controller.abort(), 5000);
  try {
    const response = await fetch(url, {
      credentials: "omit",
      redirect: "error",
      referrerPolicy: "no-referrer",
      signal: controller.signal,
    });
    if (!response.ok) throw new Error(`HTTP ${response.status}`);
    const contentType = response.headers.get("content-type") ?? "";
    if (!contentType.includes("application/json") && !contentType.includes("+json")) {
      throw new Error("Response is not JSON");
    }
    const body = await response.text();
    if (new TextEncoder().encode(body).byteLength > maxBytes) {
      throw new Error("Response is too large");
    }
    return JSON.parse(body);
  } finally {
    window.clearTimeout(timeout);
  }
}

function element(tag, className, value) {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (value !== undefined) node.textContent = value;
  return node;
}

function renderOptions(app, catalog) {
  app.replaceChildren();
  const header = element("div", "intro");
  header.append(
    element("h2", "", "Infrastructure options"),
    element("span", "count", formatCount(catalog.options.length, "provider")),
  );
  app.append(header);

  if (!catalog.options.length) {
    app.append(element("p", "empty", "No provider dependency metadata is available."));
    return;
  }

  const toolbar = element("div", "toolbar");
  const search = element("input");
  search.type = "search";
  search.placeholder = "Filter providers";
  search.setAttribute("aria-label", "Filter provider options");
  toolbar.append(search);
  app.append(toolbar);

  const options = element("div", "options");
  const cards = catalog.options.map((option, index) => renderOption(option, index === 0));
  options.append(...cards);
  app.append(options);

  const notice = element(
    "p",
    "notice",
    "ORD describes the provider resource graph. Simple Cluster still owns selection, ordering, provisioning, and deletion.",
  );
  const link = element("a", "ord-link", "Open ORD metadata");
  link.href = catalog.configUrl;
  link.target = "_blank";
  link.rel = "noopener noreferrer";
  notice.append(" ", link);
  app.append(notice);

  search.addEventListener("input", () => {
    const query = search.value.trim().toLowerCase();
    cards.forEach((card) => {
      card.hidden = !card.dataset.search.includes(query);
    });
  });
}

function renderOption({ capability, graph }, open) {
  const card = element("details", "option");
  card.open = open;
  card.dataset.search = `${capability.title} ${graph.name} ${graph.resources
    .map((resource) => `${resource.kind} ${resource.apiVersion}`)
    .join(" ")}`.toLowerCase();

  const summary = element("summary");
  const heading = element("div");
  const title = element("div", "title-row");
  title.append(
    element("h3", "", capability.title || graph.name),
    element(
      "span",
      capability.disabled ? "status status--off" : "status",
      capability.disabled ? "Unavailable" : "Available",
    ),
  );
  heading.append(
    title,
    element(
      "p",
      "meta",
      `${formatCount(graph.resources.length, "resource")} · ${formatCount(edgeCount(graph.resources), "dependency", "dependencies")}`,
    ),
  );
  summary.append(heading);
  card.append(summary);

  const body = element("div", "option-body");
  body.append(element("p", "meta legend", graph.availabilityReason));
  const resources = layoutGraph(graph.resources);
  const maxLevel = Math.max(0, ...resources.map((resource) => resource.level));
  const graphNode = element("div", "graph");
  graphNode.style.setProperty("--columns", String(maxLevel + 1));
  graphNode.setAttribute("aria-label", `${capability.title} dependency graph`);
  for (const resource of resources) {
    const node = element(
      "article",
      resource.name === graph.kubeconfigResourceName ? "node node--output" : "node",
    );
    node.style.setProperty("--column", String(resource.level + 1));
    node.append(
      element("strong", "", resource.name),
      element("span", "meta", `${resource.kind} · ${resource.apiVersion}`),
    );
    if (resource.dependsOn.length) {
      node.append(element("p", "dependency", resource.dependsOn.join(", ")));
    }
    graphNode.append(node);
  }
  body.append(graphNode);
  card.append(body);
  return card;
}

function edgeCount(resources) {
  return resources.reduce((total, resource) => total + resource.dependsOn.length, 0);
}

export function formatCount(count, singular, plural = `${singular}s`) {
  return `${count} ${count === 1 ? singular : plural}`;
}

function render(context) {
  const app = document.querySelector("#app");
  if (!app) return;
  if (context?.protocolVersion !== PROTOCOL) {
    app.replaceChildren(element("p", "error", "Provider details are unavailable."));
    return;
  }
  loadProviderOptions(context.currentProvider)
    .then((catalog) => renderOptions(app, catalog))
    .catch(() => {
      app.replaceChildren(
        element("p", "error", "Infrastructure dependency metadata is unavailable."),
      );
    });
}

let targetOrigin = "*";
function reportHeight() {
  window.parent.postMessage(
    { msg: "custom", data: { id: RESIZE, height: document.documentElement.scrollHeight } },
    targetOrigin,
  );
}

function initLuigiClient() {
  window.addEventListener("message", (event) => {
    if (event.source !== window.parent || event.data?.msg !== "luigi.init") return;
    targetOrigin = event.origin;
    window.parent.postMessage({ msg: "luigi.init.ok" }, targetOrigin);
    try {
      const raw = event.data.context;
      render(typeof raw === "string" ? JSON.parse(raw) : raw);
    } catch {
      render(undefined);
    }
  });
  window.parent.postMessage({ msg: "luigi.get-context", clientVersion: "2.23.0" }, "*");
  new ResizeObserver(reportHeight).observe(document.documentElement);
}

if (typeof window !== "undefined" && window.parent !== window) initLuigiClient();
