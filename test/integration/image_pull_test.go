//go:build integration

package integration

import (
	"fmt"
	"testing"

	"github.com/xchangeee/syslet/test/integration/systest"
)

// These tests cover pre-pulling container images. Podman would otherwise pull a
// missing image while the container service starts — after syslet has already
// stopped the old container — so the download would count as downtime. The plan
// asks podman which desired images are missing, and Apply pulls them before any
// other change.

// TestNewContainerWithMissingImage_PullsBeforeStart verifies that a missing
// image is pulled, and that the pull precedes every other effect of the apply.
func TestNewContainerWithMissingImage_PullsBeforeStart(t *testing.T) {
	env := systest.New(t)
	env.Podman.SeedMissingImages("nginx:1.28")
	env.Specs(systest.NewContainer("webapp", "nginx:1.28"))

	env.Apply()

	env.AssertImagesPulled("nginx:1.28")
	env.AssertStarted("webapp.service")
	env.AssertHappensBefore("pull-image", "nginx:1.28", "write-unit", "webapp.container")
	env.AssertHappensBefore("pull-image", "nginx:1.28", "start", "webapp.service")
}

// TestChangedImageOnRunningContainer_PullsBeforeStop verifies the case the
// feature exists for: upgrading a running container downloads the new image
// while the old container still serves.
func TestChangedImageOnRunningContainer_PullsBeforeStop(t *testing.T) {
	env := systest.New(t)
	env.Podman.SeedMissingImages("nginx:1.28")
	env.SeedActive(systest.NewContainer("webapp", "nginx:1.27"))
	env.Specs(systest.NewContainer("webapp", "nginx:1.28"))

	env.Apply()

	env.AssertImagesPulled("nginx:1.28")
	env.AssertRestarted("webapp.service")
	env.AssertHappensBefore("pull-image", "nginx:1.28", "stop", "webapp.service")
}

// TestContainerWithPresentImage_DoesNotPull verifies that an image already in
// the local store is never pulled again, not even a moving tag.
func TestContainerWithPresentImage_DoesNotPull(t *testing.T) {
	env := systest.New(t)
	env.Specs(systest.NewContainer("webapp", "nginx:latest"))

	plan := env.Plan()
	systest.AssertPlanOutputOmits(t, plan, "Images to pull:")

	env.Apply()

	env.AssertNoImagesPulled()
}

// TestContainersSharingMissingImage_PullOnce verifies that an image referenced
// by several containers is pulled once.
func TestContainersSharingMissingImage_PullOnce(t *testing.T) {
	env := systest.New(t)
	env.Podman.SeedMissingImages("nginx:1.28")
	env.Specs(
		systest.NewContainer("webapp", "nginx:1.28"),
		systest.NewContainer("sidecar", "nginx:1.28"),
	)

	env.Apply()

	env.AssertImagesPulled("nginx:1.28")
}

// TestBuiltContainer_NeverChecksOrPullsImage verifies that an Image= pointing
// at a build unit is left alone: syslet builds that image itself, and the
// ".build" reference is not something podman could pull.
func TestBuiltContainer_NeverChecksOrPullsImage(t *testing.T) {
	env := systest.New(t)
	env.Podman.FailOn = map[string]error{"image-exists:myapp.build": fmt.Errorf("must not be checked")}
	env.Specs(
		systest.NewBuild("myapp", "localhost/myapp:latest"),
		systest.NewBuiltContainer("webapp", "myapp"),
	)

	env.Apply()

	env.AssertNoImagesPulled()
}

// TestMissingImage_ShowsInPlanAndOutcome verifies that the diff lists the
// pulls and that the container's summary row mentions the pull.
func TestMissingImage_ShowsInPlanAndOutcome(t *testing.T) {
	env := systest.New(t)
	env.Podman.SeedMissingImages("nginx:1.28")
	env.SeedActive(systest.NewContainer("webapp", "nginx:1.27"))
	env.Specs(systest.NewContainer("webapp", "nginx:1.28"))

	plan := env.Plan()

	systest.AssertPlanOutputContains(t, plan,
		"\nImages to pull:\n  - nginx:1.28\n",
		"webapp.container                         updated    unit updated, image pulled, restarted (desired: running)\n")
}

// TestImagePullFails_AbortsBeforeAnyChange verifies that a failed pull stops
// the apply before it touches the host, and reports every container that
// needed the image. Continuing would restart the container into the same
// failing pull, turning a failed download into an outage.
func TestImagePullFails_AbortsBeforeAnyChange(t *testing.T) {
	env := systest.New(t)
	env.Podman.SeedMissingImages("nginx:1.28")
	env.Podman.FailOn = map[string]error{"pull-image:nginx:1.28": fmt.Errorf("registry unreachable")}
	env.SeedActive(systest.NewContainer("webapp", "nginx:1.27"))
	env.SeedActive(systest.NewContainer("sidecar", "nginx:1.27"))
	env.Specs(
		systest.NewContainer("webapp", "nginx:1.28"),
		systest.NewContainer("sidecar", "nginx:1.28"),
	)

	report, err := env.ApplyWithReport()
	if err == nil {
		t.Fatal("expected apply to fail when an image could not be pulled")
	}

	env.AssertNoEffects()
	env.AssertNoStartStop()
	systest.AssertReportOutput(t, report,
		"webapp.container                         error      pulling image: registry unreachable\n"+
			"sidecar.container                        error      pulling image: registry unreachable\n")
}

// TestImageExistsCheckFails_RefusesPlan verifies that a podman failure while
// checking the image store fails the plan for that container instead of
// guessing whether the image is there.
func TestImageExistsCheckFails_RefusesPlan(t *testing.T) {
	env := systest.New(t)
	env.Podman.FailOn = map[string]error{"image-exists:nginx:1.28": fmt.Errorf("podman broke")}
	env.Specs(systest.NewContainer("webapp", "nginx:1.28"))

	perr := env.PlanErrors()

	systest.AssertPlanErrorsContain(t, perr, "checking image nginx:1.28: podman broke")
}

// TestMissingImage_IsIdempotent verifies that a pulled image is not pulled
// again on the next run.
func TestMissingImage_IsIdempotent(t *testing.T) {
	env := systest.New(t)
	env.Podman.SeedMissingImages("nginx:1.28")
	env.Specs(systest.NewContainer("webapp", "nginx:1.28"))

	env.AssertIdempotent()
}
