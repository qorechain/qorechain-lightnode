/* QoreChain Light Node — Dashboard Application */
"use strict";

document.addEventListener("alpine:init", () => {
    Alpine.data("app", () => ({
        page: "overview",
        sidebarOpen: false,

        // Overview data
        status: null,
        recentHeaders: [],
        chartInstance: null,
        chartData: { times: [], heights: [] },

        // Validators
        validators: [],

        // Delegation
        delegations: [],
        splitConfig: null,
        autoCompound: false,
        rebalanceAlerts: [],

        // Network
        networkPeers: 0,
        syncStatus: null,

        // Bridge
        bridgeConnections: [],
        bridgeActivity: [],

        // Tokenomics
        burnStats: null,
        feeDistribution: [],
        inflationInfo: null,

        // Settings
        config: null,

        // WebSocket
        ws: null,
        wsConnected: false,

        init() {
            this.connectWS();
            this.loadPage("overview");
        },

        navigate(p) {
            this.page = p;
            this.sidebarOpen = false;
            this.loadPage(p);
        },

        async loadPage(p) {
            try {
                switch (p) {
                    case "overview":
                        await this.fetchOverview();
                        break;
                    case "validators":
                        await this.fetchValidators();
                        break;
                    case "delegation":
                        await this.fetchDelegation();
                        break;
                    case "network":
                        await this.fetchNetwork();
                        break;
                    case "bridge":
                        await this.fetchBridge();
                        break;
                    case "tokenomics":
                        await this.fetchTokenomics();
                        break;
                    case "settings":
                        await this.fetchSettings();
                        break;
                }
            } catch (e) {
                console.warn("Failed to load page data:", p, e);
            }
        },

        /* ── Data Fetching ─────────────────────────── */

        async fetchOverview() {
            const [statusRes, headersRes] = await Promise.allSettled([
                fetch("/api/status"),
                fetch("/api/headers/recent"),
            ]);
            if (statusRes.status === "fulfilled" && statusRes.value.ok) {
                this.status = await statusRes.value.json();
            }
            if (headersRes.status === "fulfilled" && headersRes.value.ok) {
                this.recentHeaders = await headersRes.value.json();
                this.updateChartData();
            }
        },

        async fetchValidators() {
            const res = await fetch("/api/validators");
            if (res.ok) {
                this.validators = await res.json();
            }
        },

        async fetchDelegation() {
            const [delRes, splitRes] = await Promise.allSettled([
                fetch("/api/delegation"),
                fetch("/api/delegation/split"),
            ]);
            if (delRes.status === "fulfilled" && delRes.value.ok) {
                const data = await delRes.value.json();
                this.delegations = data.delegations || [];
                this.autoCompound = data.auto_compound || false;
                this.rebalanceAlerts = data.rebalance_alerts || [];
            }
            if (splitRes.status === "fulfilled" && splitRes.value.ok) {
                this.splitConfig = await splitRes.value.json();
            }
        },

        async fetchNetwork() {
            const res = await fetch("/api/network");
            if (res.ok) {
                const data = await res.json();
                this.networkPeers = data.peers || 0;
                this.syncStatus = data.sync || null;
            }
            this.$nextTick(() => this.renderBlockChart());
        },

        async fetchBridge() {
            const [connRes, actRes] = await Promise.allSettled([
                fetch("/api/bridge/connections"),
                fetch("/api/bridge/activity"),
            ]);
            if (connRes.status === "fulfilled" && connRes.value.ok) {
                this.bridgeConnections = await connRes.value.json();
            }
            if (actRes.status === "fulfilled" && actRes.value.ok) {
                this.bridgeActivity = await actRes.value.json();
            }
        },

        async fetchTokenomics() {
            const res = await fetch("/api/tokenomics");
            if (res.ok) {
                const data = await res.json();
                this.burnStats = data.burn || null;
                this.feeDistribution = data.fee_distribution || [];
                this.inflationInfo = data.inflation || null;
            }
        },

        async fetchSettings() {
            const res = await fetch("/api/config");
            if (res.ok) {
                this.config = await res.json();
            }
        },

        /* ── WebSocket ─────────────────────────────── */

        connectWS() {
            const proto = location.protocol === "https:" ? "wss" : "ws";
            const url = proto + "://" + location.host + "/ws";
            try {
                this.ws = new WebSocket(url);
            } catch (_) {
                this.scheduleReconnect();
                return;
            }
            this.ws.onopen = () => {
                this.wsConnected = true;
            };
            this.ws.onclose = () => {
                this.wsConnected = false;
                this.scheduleReconnect();
            };
            this.ws.onerror = () => {
                this.ws.close();
            };
            this.ws.onmessage = (evt) => {
                try {
                    const msg = JSON.parse(evt.data);
                    this.handleWSMessage(msg);
                } catch (_) {
                    // ignore malformed messages
                }
            };
        },

        scheduleReconnect() {
            setTimeout(() => this.connectWS(), 3000);
        },

        handleWSMessage(msg) {
            switch (msg.type) {
                case "status":
                    this.status = msg.data;
                    break;
                case "header":
                    this.recentHeaders.unshift(msg.data);
                    if (this.recentHeaders.length > 50) {
                        this.recentHeaders.pop();
                    }
                    this.pushChartPoint(msg.data);
                    break;
                case "validators":
                    this.validators = msg.data;
                    break;
            }
        },

        /* ── Charts ────────────────────────────────── */

        updateChartData() {
            this.chartData.times = [];
            this.chartData.heights = [];
            const sorted = this.recentHeaders.slice().reverse();
            for (let i = 0; i < sorted.length; i++) {
                const h = sorted[i];
                this.chartData.times.push(
                    Math.floor(new Date(h.time).getTime() / 1000)
                );
                this.chartData.heights.push(h.height);
            }
            this.$nextTick(() => this.renderActivityChart());
        },

        pushChartPoint(header) {
            const t = Math.floor(new Date(header.time).getTime() / 1000);
            this.chartData.times.push(t);
            this.chartData.heights.push(header.height);
            if (this.chartData.times.length > 60) {
                this.chartData.times.shift();
                this.chartData.heights.shift();
            }
            this.renderActivityChart();
        },

        renderActivityChart() {
            const el = document.getElementById("activity-chart");
            if (!el || typeof uPlot === "undefined") return;
            if (this.chartData.times.length < 2) return;

            if (this.chartInstance) {
                this.chartInstance.destroy();
                this.chartInstance = null;
            }

            const opts = {
                width: el.clientWidth || 500,
                height: 200,
                cursor: { show: true },
                scales: { x: { time: true }, y: { auto: true } },
                axes: [
                    {
                        stroke: "#6b7280",
                        grid: { stroke: "rgba(31,41,55,0.5)" },
                        ticks: { stroke: "rgba(31,41,55,0.5)" },
                    },
                    {
                        stroke: "#6b7280",
                        grid: { stroke: "rgba(31,41,55,0.5)" },
                        ticks: { stroke: "rgba(31,41,55,0.5)" },
                    },
                ],
                series: [
                    {},
                    {
                        label: "Block Height",
                        stroke: "#3da8ff",
                        width: 2,
                        fill: "rgba(61,168,255,0.08)",
                    },
                ],
            };

            this.chartInstance = new uPlot(
                opts,
                [this.chartData.times, this.chartData.heights],
                el
            );
        },

        renderBlockChart() {
            const el = document.getElementById("block-chart");
            if (!el || typeof uPlot === "undefined") return;
            if (this.chartData.times.length < 2) return;

            el.innerHTML = "";
            const opts = {
                width: el.clientWidth || 500,
                height: 240,
                cursor: { show: true },
                scales: { x: { time: true }, y: { auto: true } },
                axes: [
                    {
                        stroke: "#6b7280",
                        grid: { stroke: "rgba(31,41,55,0.5)" },
                        ticks: { stroke: "rgba(31,41,55,0.5)" },
                    },
                    {
                        stroke: "#6b7280",
                        grid: { stroke: "rgba(31,41,55,0.5)" },
                        ticks: { stroke: "rgba(31,41,55,0.5)" },
                    },
                ],
                series: [
                    {},
                    {
                        label: "Height",
                        stroke: "#10b981",
                        width: 2,
                        fill: "rgba(16,185,129,0.08)",
                    },
                ],
            };

            new uPlot(
                opts,
                [this.chartData.times, this.chartData.heights],
                el
            );
        },

        /* ── Helpers ───────────────────────────────── */

        fmtNumber(n) {
            if (n == null) return "--";
            return Number(n).toLocaleString();
        },

        fmtQOR(uqor) {
            if (uqor == null) return "--";
            return (
                (Number(uqor) / 1e6).toLocaleString(undefined, {
                    minimumFractionDigits: 2,
                    maximumFractionDigits: 2,
                }) + " QOR"
            );
        },

        fmtPercent(v) {
            if (v == null) return "--";
            return (Number(v) * 100).toFixed(1) + "%";
        },

        fmtTime(t) {
            if (!t) return "--";
            return new Date(t).toLocaleTimeString();
        },

        shortenHash(h) {
            if (!h || h.length < 12) return h || "--";
            return h.slice(0, 6) + "..." + h.slice(-6);
        },

        statusClass(s) {
            if (!s) return "disconnected";
            if (s.syncing) return "syncing";
            return "connected";
        },

        statusLabel(s) {
            if (!s) return "Offline";
            if (s.syncing) return "Syncing";
            return "Synced";
        },

        pieStyle(distribution) {
            if (!distribution || distribution.length === 0) return {};
            const colors = [
                "#3da8ff",
                "#10b981",
                "#f59e0b",
                "#ef4444",
                "#8b5cf6",
                "#ec4899",
            ];
            let gradient = "conic-gradient(";
            let cumulative = 0;
            for (let i = 0; i < distribution.length; i++) {
                const item = distribution[i];
                const start = cumulative;
                cumulative += (item.percent || 0) * 100;
                const color = colors[i % colors.length];
                gradient += color + " " + start + "% " + cumulative + "%";
                if (i < distribution.length - 1) gradient += ", ";
            }
            gradient += ")";
            return { background: gradient };
        },

        configJSON() {
            if (!this.config) return "Loading configuration...";
            return JSON.stringify(this.config, null, 2);
        },

        navItems: [
            { id: "overview", label: "Overview", icon: "chart" },
            { id: "validators", label: "Validators", icon: "shield" },
            { id: "delegation", label: "Delegation", icon: "layers" },
            { id: "network", label: "Network", icon: "globe" },
            { id: "bridge", label: "Bridge", icon: "link" },
            { id: "tokenomics", label: "Tokenomics", icon: "coin" },
            { id: "settings", label: "Settings", icon: "gear" },
        ],
    }));
});

/* ── SVG Icon Helper (called from HTML templates) ── */

function navIcon(name) {
    var icons = {
        chart: '<path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M3 3v18h18M7 16l4-4 4 4 5-5"/>',
        shield: '<path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/>',
        layers: '<path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M12 2L2 7l10 5 10-5-10-5zM2 17l10 5 10-5M2 12l10 5 10-5"/>',
        globe: '<circle cx="12" cy="12" r="10" stroke-width="1.5" fill="none"/><path stroke-width="1.5" d="M2 12h20M12 2a15.3 15.3 0 014 10 15.3 15.3 0 01-4 10 15.3 15.3 0 01-4-10A15.3 15.3 0 0112 2z"/>',
        link: '<path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M10 13a5 5 0 007.54.54l3-3a5 5 0 00-7.07-7.07l-1.72 1.71M14 11a5 5 0 00-7.54-.54l-3 3a5 5 0 007.07 7.07l1.71-1.71"/>',
        coin: '<circle cx="12" cy="12" r="10" stroke-width="1.5" fill="none"/><path stroke-width="1.5" d="M12 6v12M8 10h8M8 14h8"/>',
        gear: '<circle cx="12" cy="12" r="3" stroke-width="1.5" fill="none"/><path stroke-width="1.5" d="M19.4 15a1.65 1.65 0 00.33 1.82l.06.06a2 2 0 01-2.83 2.83l-.06-.06a1.65 1.65 0 00-1.82-.33 1.65 1.65 0 00-1 1.51V21a2 2 0 01-4 0v-.09A1.65 1.65 0 009 19.4a1.65 1.65 0 00-1.82.33l-.06.06a2 2 0 01-2.83-2.83l.06-.06A1.65 1.65 0 004.68 15a1.65 1.65 0 00-1.51-1H3a2 2 0 010-4h.09A1.65 1.65 0 004.6 9a1.65 1.65 0 00-.33-1.82l-.06-.06a2 2 0 012.83-2.83l.06.06A1.65 1.65 0 009 4.68a1.65 1.65 0 001-1.51V3a2 2 0 014 0v.09a1.65 1.65 0 001 1.51 1.65 1.65 0 001.82-.33l.06-.06a2 2 0 012.83 2.83l-.06.06A1.65 1.65 0 0019.4 9a1.65 1.65 0 001.51 1H21a2 2 0 010 4h-.09a1.65 1.65 0 00-1.51 1z"/>',
    };
    var svg =
        '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor">';
    svg += icons[name] || "";
    svg += "</svg>";
    return svg;
}
