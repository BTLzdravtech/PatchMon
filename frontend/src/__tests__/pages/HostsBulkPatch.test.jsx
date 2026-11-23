import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
	fireEvent,
	render,
	screen,
	waitFor,
	within,
} from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import Hosts from "../../pages/Hosts";
import { dashboardAPI, userPreferencesAPI } from "../../utils/api";

const { HOSTS } = vi.hoisted(() => {
	const host = (overrides) => ({
		api_id: `api-${overrides.id}`,
		os_type: "Ubuntu",
		os_version: "22.04",
		status: "active",
		host_group_memberships: [],
		updatesCount: 0,
		securityUpdatesCount: 0,
		last_update: new Date().toISOString(),
		config_version: null,
		reported_host_id: null,
		identity_conflict_at: null,
		...overrides,
	});

	const HOSTS = [
		host({
			id: "h-ok",
			friendly_name: "healthy",
			agent_version: "2.1.4",
			config_version: "2.1.4",
		}),
		host({
			id: "h-stale",
			friendly_name: "stale-config",
			agent_version: "2.1.4",
			config_version: "2.1.4-rc.1",
		}),
		host({ id: "h-old", friendly_name: "old-agent", agent_version: "2.1.3" }),
		host({
			id: "h-clone",
			friendly_name: "cloned",
			agent_version: "2.1.4",
			config_version: "2.1.4",
			identity_conflict_at: "2026-10-13T10:00:00Z",
		}),
		host({
			id: "h-wrongid",
			friendly_name: "wrong-id",
			agent_version: "2.1.4",
			config_version: "2.1.4",
			reported_host_id: "h-ok",
		}),
	];
	return { HOSTS };
});

// Fork: Hosts gates its bulk actions on useAuth().canManageHosts.
vi.mock("../../contexts/AuthContext", () => ({
	useAuth: () => ({ canManageHosts: () => true }),
}));

vi.mock("../../contexts/ToastContext", () => ({
	useToast: () => ({
		warning: vi.fn(),
		success: vi.fn(),
		error: vi.fn(),
		info: vi.fn(),
	}),
}));

vi.mock("../../utils/api", async (importOriginal) => {
	const actual = await importOriginal();
	const ok = (data) => vi.fn(() => Promise.resolve({ data }));
	return {
		...actual,
		dashboardAPI: {
			getHosts: ok({ items: HOSTS, total: HOSTS.length, limit: 50, offset: 0 }),
			getHostFilterOptions: ok({ osDistribution: [] }),
			getHostCounts: ok({ total: HOSTS.length }),
			getWsStatusSummary: ok({}),
		},
		hostGroupsAPI: { list: ok([]) },
		settingsAPI: { getPublic: ok({}), get: ok({}), update: ok({}) },
		userPreferencesAPI: { get: ok({}), update: ok({}) },
		alertsAPI: { getAlertConfigByType: ok({ data: {} }) },
		adminHostsAPI: {},
	};
});

const renderHosts = () => {
	const queryClient = new QueryClient({
		defaultOptions: { queries: { retry: false } },
	});
	return render(
		<QueryClientProvider client={queryClient}>
			<MemoryRouter initialEntries={["/hosts"]}>
				<Hosts />
			</MemoryRouter>
		</QueryClientProvider>,
	);
};

const desktopRow = (name) =>
	screen
		.getAllByText(name)
		.map((el) => el.closest("tr"))
		.find(Boolean);

beforeEach(() => {
	localStorage.clear();
	userPreferencesAPI.get.mockImplementation(() =>
		Promise.resolve({ data: {} }),
	);
	vi.stubGlobal(
		"fetch",
		vi.fn(() =>
			Promise.resolve({ ok: true, json: () => Promise.resolve({ data: {} }) }),
		),
	);
});

afterEach(() => {
	vi.unstubAllGlobals();
});

// Rendering the whole Hosts page is slow when the suite runs in parallel; the
// default 1s waitFor and 5s test limits are not enough there.
const SLOW = { timeout: 15000 };

describe("Hosts bulk patch", () => {
	it("opens the patch wizard for the selected hosts", async () => {
		renderHosts();
		await screen.findAllByText("healthy", {}, SLOW);
		for (const name of ["healthy", "old-agent"]) {
			fireEvent.click(within(desktopRow(name)).getAllByRole("button")[0]);
		}
		fireEvent.click(
			await screen.findByRole("button", { name: /patch selected/i }, SLOW),
		);
		await waitFor(
			() => expect(document.body.querySelector(".z-\\[120\\]")).not.toBeNull(),
			SLOW,
		);
		expect(
			within(document.body.querySelector(".z-\\[120\\]")).getByText(
				"old-agent",
			),
		).toBeInTheDocument();
	}, 30000);

	// The selected-hosts lookup keeps the previous selection as placeholder
	// data while it refetches. The host just ticked is missing from that, and
	// must not be shown as a skipped (Windows) host in the meantime.
	it("does not count a host whose details are still loading as skipped", async () => {
		const pending = [];
		dashboardAPI.getHosts.mockImplementation((params = {}) => {
			if (params.filter !== "selected") {
				return Promise.resolve({
					data: { items: HOSTS, total: HOSTS.length, limit: 50, offset: 0 },
				});
			}
			const ids = params.selected.split(",");
			return new Promise((resolve) => {
				pending.push(() =>
					resolve({
						data: { items: HOSTS.filter((h) => ids.includes(h.id)) },
					}),
				);
			});
		});

		renderHosts();
		await screen.findAllByText("healthy", {}, SLOW);
		const select = (name) =>
			fireEvent.click(within(desktopRow(name)).getAllByRole("button")[0]);
		const patchButton = () =>
			screen.getByRole("button", { name: /patch selected/i });

		select("healthy");
		await waitFor(() => expect(pending).toHaveLength(1), SLOW);
		pending[0]();
		await waitFor(() => expect(patchButton()).toBeEnabled(), SLOW);

		// Second host ticked; its lookup is left unanswered.
		select("old-agent");
		await waitFor(() => expect(pending).toHaveLength(2), SLOW);
		expect(patchButton()).toHaveTextContent(/^Patch Selected\s*Patch$/);
		expect(patchButton().title).toBe(
			"Patch all available updates on the selected hosts",
		);
	}, 30000);
});
