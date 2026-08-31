const PROTOCOL = "platform-mesh.provider-details.v1";
const NAVIGATE = "platform-mesh.provider-details.navigate.v1";
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

export function providerIconUrl(provider, prefersDark = false) {
  const icon = provider?.providerMetadata?.spec?.icon;
  const preferred = prefersDark ? icon?.dark : icon?.light;
  const fallback = prefersDark ? icon?.light : icon?.dark;
  return [preferred?.url, preferred?.data, fallback?.url, fallback?.data]
    .map(safeImageUrl)
    .find(Boolean);
}

export function matchMarketplaceProvider(graphName, providers = []) {
  const family = tokens(graphName)[0];
  if (!family) return undefined;
  return providers.find((provider) => {
    const metadata = provider?.providerMetadata;
    const identities = [
      provider?.name,
      metadata?.metadata?.name,
      metadata?.spec?.displayName,
    ];
    return identities.some((identity) => tokens(identity).includes(family));
  });
}

export async function loadProviderOptions(provider, fetchJSON = fetchJson) {
  const configUrl = configUrlFor(provider);
  if (!configUrl) throw new Error("No ORD configuration URL");
  const configuration = await fetchJSON(configUrl, 256 * 1024);
  const declarations = configuration?.openResourceDiscoveryV1?.documents;
  if (!Array.isArray(declarations))
    throw new Error("Invalid ORD configuration");

  const documents = await Promise.all(
    declarations
      .filter((item) =>
        (item.accessStrategies ?? []).some(
          (strategy) => strategy.type === "open",
        ),
      )
      .slice(0, MAX_DOCUMENTS)
      .map(async (item) => {
        const document = await fetchJSON(
          new URL(item.url, configUrl).href,
          MAX_BYTES,
        );
        if (document?.openResourceDiscovery !== "1.16") {
          throw new Error("Unsupported ORD version");
        }
        return document;
      }),
  );

  const capabilities = documents.flatMap((document) =>
    (document.capabilities ?? []).filter(
      (item) => item.type === CAPABILITY_TYPE,
    ),
  );
  const loaded = await Promise.allSettled(
    capabilities.map(async (capability) => {
      const definition = capability.definitions?.find(
        (item) =>
          item.mediaType === "application/json" &&
          (item.accessStrategies ?? []).some(
            (strategy) => strategy.type === "open",
          ),
      );
      if (!definition) throw new Error("Provider graph definition is missing");
      const graph = await fetchJSON(
        new URL(definition.url, configUrl).href,
        MAX_BYTES,
      );
      validateGraph(graph);
      return { capability, graph };
    }),
  );
  return {
    configUrl,
    options: loaded
      .filter((result) => result.status === "fulfilled")
      .map((result) => result.value)
      .sort((left, right) =>
        left.capability.title.localeCompare(right.capability.title),
      ),
  };
}

export function layoutGraph(resources) {
  const byName = new Map(
    resources.map((resource) => [resource.name, resource]),
  );
  const memo = new Map();
  const visiting = new Set();
  function level(name) {
    if (memo.has(name)) return memo.get(name);
    if (visiting.has(name)) return 0;
    visiting.add(name);
    const resource = byName.get(name);
    const dependencies = (resource?.dependsOn ?? []).filter((dependency) =>
      byName.has(dependency),
    );
    const value = dependencies.length
      ? 1 + Math.max(...dependencies.map((dependency) => level(dependency)))
      : 0;
    visiting.delete(name);
    memo.set(name, value);
    return value;
  }
  return resources.map((resource) => ({
    ...resource,
    level: level(resource.name),
  }));
}

function validateGraph(graph) {
  if (
    !graph ||
    typeof graph.name !== "string" ||
    !Array.isArray(graph.resources)
  ) {
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
    if (
      !contentType.includes("application/json") &&
      !contentType.includes("+json")
    ) {
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

function safeImageUrl(value) {
  if (typeof value !== "string") return undefined;
  if (
    /^data:image\/(?:gif|jpeg|png|svg\+xml|webp);base64,[a-z0-9+/=\s]+$/i.test(
      value,
    )
  ) {
    return value;
  }
  try {
    const parsed = new URL(value);
    return parsed.protocol === "https:" ? parsed.href : undefined;
  } catch {
    return undefined;
  }
}

function tokens(value) {
  return typeof value === "string"
    ? (value.toLowerCase().match(/[a-z0-9]+/g) ?? [])
    : [];
}

function element(tag, className, value) {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (value !== undefined) node.textContent = value;
  return node;
}

function renderOptions(app, catalog, providers) {
  app.replaceChildren();
  const panel = element("section", "panel");
  const header = element("header", "panel__header");
  const title = element("div", "panel__title");
  title.append(
    element("h2", "eyebrow", "Simple Cluster"),
    element("p", "panel__name", "Infrastructure providers"),
  );
  header.append(
    title,
    element("span", "count", formatCount(catalog.options.length, "provider")),
  );
  panel.append(header);

  if (!catalog.options.length) {
    panel.append(
      element("p", "empty", "No infrastructure providers are available."),
    );
    app.append(panel);
    return;
  }

  const results = element("div", "results");
  const flow = element("section", "flow");
  const rows = catalog.options.map((option) => {
    const marketplaceProvider = matchMarketplaceProvider(
      option.graph.name,
      providers,
    );
    const row = renderProviderRow(option, marketplaceProvider);
    row.addEventListener("click", () => {
      rows.forEach((candidate) => {
        const selected = candidate === row;
        candidate.classList.toggle("result--selected", selected);
        candidate.setAttribute("aria-pressed", String(selected));
      });
      flow.replaceChildren(renderFlow(option, marketplaceProvider));
    });
    return row;
  });
  results.append(...rows);
  panel.append(results, flow);

  const footer = element("p", "ord-footer", "Source: ");
  const link = element("a", "ord-link", "ORD metadata");
  link.href = catalog.configUrl;
  link.target = "_blank";
  link.rel = "noopener noreferrer";
  footer.append(link);
  panel.append(footer);
  app.append(panel);

  const initial = Math.max(
    0,
    catalog.options.findIndex(({ capability }) => !capability.disabled),
  );
  rows[initial].click();
}

function renderProviderRow({ capability, graph }, marketplaceProvider) {
  const row = element("button", "result");
  row.type = "button";
  row.setAttribute("aria-pressed", "false");
  const displayName =
    marketplaceProvider?.providerMetadata?.spec?.displayName ||
    capability.title ||
    graph.name;
  row.append(renderProviderIcon(marketplaceProvider, displayName));

  const title = element("span", "result__title");
  title.append(
    element("strong", "", displayName),
    element(
      "span",
      "meta",
      `${capability.title || graph.name} · ${formatCount(graph.resources.length, "resource")}`,
    ),
  );
  const badges = element("span", "badges");
  badges.append(
    element(
      "span",
      capability.disabled ? "badge badge--off" : "badge",
      capability.disabled ? "Unavailable" : "Available",
    ),
  );
  const arrow = element("span", "arrow", "›");
  arrow.setAttribute("aria-hidden", "true");
  row.append(title, badges, arrow);
  return row;
}

function renderFlow({ capability, graph }, marketplaceProvider) {
  const fragment = document.createDocumentFragment();
  const header = element("header", "flow__header");
  const title = element("div", "flow__title");
  title.append(
    element("h3", "eyebrow", "Provisioning flow"),
    element("p", "panel__name", capability.title || graph.name),
  );
  header.append(title);
  if (marketplaceProvider) {
    const open = element("button", "provider-link", "Open provider");
    open.type = "button";
    open.addEventListener("click", () =>
      sendMessage(NAVIGATE, { providerName: marketplaceProvider.name }),
    );
    header.append(open);
  }

  const body = element("div", "flow__body");
  body.append(
    element(
      "p",
      "meta flow__hint",
      "Simple Cluster provisions these resources in dependency order.",
    ),
  );
  const resources = layoutGraph(graph.resources);
  const maxLevel = Math.max(0, ...resources.map((resource) => resource.level));
  const stages = element("div", "flow__stages");
  stages.style.setProperty("--columns", String(maxLevel + 1));
  stages.setAttribute("aria-label", `${capability.title} dependency graph`);
  for (let level = 0; level <= maxLevel; level += 1) {
    const stage = element("div", "flow__stage");
    for (const resource of resources.filter(
      (candidate) => candidate.level === level,
    )) {
      const node = element(
        "article",
        resource.name === graph.kubeconfigResourceName
          ? "node node--output"
          : "node",
      );
      node.append(
        element("strong", "", resource.name),
        element("span", "meta", `${resource.kind} · ${resource.apiVersion}`),
      );
      if (resource.dependsOn.length) {
        node.append(
          element(
            "span",
            "dependency",
            `Depends on ${resource.dependsOn.join(", ")}`,
          ),
        );
      }
      stage.append(node);
    }
    stages.append(stage);
  }
  body.append(stages);
  fragment.append(header, body);
  return fragment;
}

function renderProviderIcon(provider, displayName) {
  const visual = element("span", "provider-visual");
  visual.setAttribute("aria-hidden", "true");
  const initials = tokens(displayName)
    .slice(0, 2)
    .map((part) => part[0])
    .join("")
    .toUpperCase();
  const showFallback = () => {
    visual.replaceChildren();
    visual.classList.add("provider-visual--fallback");
    visual.textContent = initials || "MSP";
  };
  const iconUrl = providerIconUrl(provider);
  if (iconUrl) {
    const icon = element("img", "provider-icon");
    icon.src = iconUrl;
    icon.alt = "";
    icon.loading = "lazy";
    icon.decoding = "async";
    icon.referrerPolicy = "no-referrer";
    icon.addEventListener("error", showFallback, { once: true });
    visual.append(icon);
  } else {
    showFallback();
  }
  return visual;
}

export function formatCount(count, singular, plural = `${singular}s`) {
  return `${count} ${count === 1 ? singular : plural}`;
}

function render(context) {
  const app = document.querySelector("#app");
  if (!app) return;
  if (context?.protocolVersion !== PROTOCOL) {
    app.replaceChildren(
      element("p", "error", "Provider details are unavailable."),
    );
    return;
  }
  loadProviderOptions(context.currentProvider)
    .then((catalog) => renderOptions(app, catalog, context.providers ?? []))
    .catch(() => {
      app.replaceChildren(
        element(
          "p",
          "error",
          "Infrastructure dependency metadata is unavailable.",
        ),
      );
    });
}

let targetOrigin = "*";

function sendMessage(id, data) {
  window.parent.postMessage(
    { msg: "custom", data: { id, ...data } },
    targetOrigin,
  );
}

function reportHeight() {
  sendMessage(RESIZE, { height: document.documentElement.scrollHeight });
}

function initLuigiClient() {
  window.addEventListener("message", (event) => {
    if (event.source !== window.parent || event.data?.msg !== "luigi.init")
      return;
    targetOrigin = event.origin;
    window.parent.postMessage({ msg: "luigi.init.ok" }, targetOrigin);
    try {
      const raw = event.data.context;
      render(typeof raw === "string" ? JSON.parse(raw) : raw);
    } catch {
      render(undefined);
    }
  });
  window.parent.postMessage(
    { msg: "luigi.get-context", clientVersion: "2.23.0" },
    "*",
  );
  new ResizeObserver(reportHeight).observe(document.documentElement);
}

if (typeof window !== "undefined" && window.parent !== window)
  initLuigiClient();
