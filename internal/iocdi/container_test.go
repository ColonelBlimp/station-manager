package iocdi

import (
	"errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"reflect"
	"strings"
	"testing"
)

type TestSuite struct {
	suite.Suite
}

type Service struct {
	Config *Config `di.inject:"ServiceBeanConfig"`
	Logger *Logger `di.inject:"ServiceBeanLogger"`
}

type Config struct {
	WorkingDir string `di.inject:"WorkingDir"`
}

type Logger struct{ _ byte }

// Additional test-only types to verify tag-only injection behavior.
type untaggedReceiver struct {
	// Intentionally no di.inject tag; should never be injected.
	Logger *Logger
	// Tagged dependency; should be injected.
	Config *Config `di.inject:"ServiceBeanConfig"`
}

type dualLoggerReceiver struct {
	// Two fields of the same type, disambiguated by tags.
	L1 *Logger `di.inject:"LoggerA"`
	L2 *Logger `di.inject:"LoggerB"`
}

func (suite *TestSuite) SetupTest() {
}

func TestContainerTestSuite(t *testing.T) {
	suite.Run(t, new(TestSuite))
}

func (suite *TestSuite) TestContainer() {
	container := New()
	assert.NotNil(suite.T(), container)

	err := container.Register("ServiceBean", reflect.TypeOf((*Service)(nil)))
	assert.NoError(suite.T(), err)
	err = container.Register("ServiceBeanConfig", reflect.TypeOf((*Config)(nil)))
	assert.NoError(suite.T(), err)
	err = container.Register("ServiceBeanLogger", reflect.TypeOf((*Logger)(nil)))
	assert.NoError(suite.T(), err)
	err = container.RegisterInstance("WorkingDir", "/home/user/test")
	assert.NoError(suite.T(), err)

	err = container.Build()
	assert.NoError(suite.T(), err)
}

func (suite *TestSuite) TestInject_PointerFields_FromStructValueInstances() {
	// Service has *Config and *Logger fields.
	// Register dependencies as struct VALUES to ensure normalization to pointers and successful injection.
	c := New()

	// Register Service by type (pointer-to-struct type)
	err := c.Register("ServiceBean", reflect.TypeOf((*Service)(nil)))
	assert.NoError(suite.T(), err)

	// Register dependencies as struct values (will be normalized to pointers)
	err = c.RegisterInstance("ServiceBeanConfig", Config{})
	assert.NoError(suite.T(), err)
	err = c.RegisterInstance("ServiceBeanLogger", Logger{})
	assert.NoError(suite.T(), err)

	// Register the simple string dependency
	err = c.RegisterInstance("WorkingDir", "/tmp/app")
	assert.NoError(suite.T(), err)

	// Build container and inject
	err = c.Build()
	assert.NoError(suite.T(), err)

	// Validate: Service instance exists and its pointer fields are injected
	b, ok := c.registeredBeans[strings.ToLower("ServiceBean")]
	assert.True(suite.T(), ok)
	assert.NotNil(suite.T(), b.instance)

	svc, ok := b.instance.(*Service)
	assert.True(suite.T(), ok)

	assert.NotNil(suite.T(), svc.Config)
	assert.Equal(suite.T(), "/tmp/app", svc.Config.WorkingDir)
	assert.NotNil(suite.T(), svc.Logger)
}

func (suite *TestSuite) TestInject_WhenReceiverRegisteredAsStructValueInstance() {
	// Register the receiver (Service) as a struct VALUE instance to ensure it is normalized to a pointer
	// and still has its dependencies injected.
	c := New()

	// Register Service as instance (struct value)
	err := c.RegisterInstance("ServiceBean", Service{})
	assert.NoError(suite.T(), err)

	// Register dependencies (mix of pointer and struct value)
	err = c.RegisterInstance("ServiceBeanConfig", &Config{})
	assert.NoError(suite.T(), err)
	err = c.RegisterInstance("ServiceBeanLogger", Logger{})
	assert.NoError(suite.T(), err)
	err = c.RegisterInstance("WorkingDir", "/var/lib/app")
	assert.NoError(suite.T(), err)

	// Build container and inject
	err = c.Build()
	assert.NoError(suite.T(), err)

	// Validate: Service instance exists and its pointer fields are injected
	b, ok := c.registeredBeans["servicebean"]
	assert.True(suite.T(), ok)
	assert.NotNil(suite.T(), b.instance)

	svc, ok := b.instance.(*Service)
	assert.True(suite.T(), ok)

	assert.NotNil(suite.T(), svc.Config)
	assert.Equal(suite.T(), "/var/lib/app", svc.Config.WorkingDir)
	assert.NotNil(suite.T(), svc.Logger)
}

// Tag-only injection: untagged fields must be ignored even if a compatible bean exists.
func TestTagOnlyInjection_UntaggedFieldIgnored(t *testing.T) {
	c := New()

	// Register receiver by type.
	err := c.Register("ReceiverBean", reflect.TypeOf((*untaggedReceiver)(nil)))
	require.NoError(t, err)

	// Register tagged dependency for Config and its literal dependency.
	err = c.RegisterInstance("ServiceBeanConfig", &Config{})
	require.NoError(t, err)
	err = c.RegisterInstance("WorkingDir", "/opt/app")
	require.NoError(t, err)

	// Also register a Logger bean that WOULD match by type, but should not be injected
	// because the receiver field is not tagged.
	err = c.RegisterInstance("ServiceBeanLogger", &Logger{})
	require.NoError(t, err)

	// Build and inject.
	err = c.Build()
	require.NoError(t, err)

	// Validate
	b, ok := c.registeredBeans["receiverbean"]
	require.True(t, ok)
	require.NotNil(t, b.instance)

	recv, ok := b.instance.(*untaggedReceiver)
	require.True(t, ok)

	// Config is tagged and should be injected (including its WorkingDir).
	require.NotNil(t, recv.Config)
	require.Equal(t, "/opt/app", recv.Config.WorkingDir)

	// Logger has no tag and MUST remain nil even though a compatible bean exists.
	require.Nil(t, recv.Logger)
}

// Tag-only injection: when multiple fields share the same type, tags disambiguate them.
func TestTagOnlyInjection_MultipleSameTypeQualified(t *testing.T) {
	c := New()

	err := c.Register("ReceiverBean", reflect.TypeOf((*dualLoggerReceiver)(nil)))
	require.NoError(t, err)

	// Register two distinct Logger beans with different ids.
	err = c.RegisterInstance("LoggerA", &Logger{})
	require.NoError(t, err)
	err = c.RegisterInstance("LoggerB", &Logger{})
	require.NoError(t, err)

	// Build and inject.
	err = c.Build()
	require.NoError(t, err)

	// Validate
	b, ok := c.registeredBeans["receiverbean"]
	require.True(t, ok)
	require.NotNil(t, b.instance)

	recv, ok := b.instance.(*dualLoggerReceiver)
	require.True(t, ok)

	// Both fields should be injected with their respective beans.
	require.NotNil(t, recv.L1)
	require.NotNil(t, recv.L2)
	// Ensure they are distinct instances corresponding to different registrations.
	require.NotSame(t, recv.L1, recv.L2)
}

//func TestLiteralProvider_InjectsStringIntoConfig(t *testing.T) {
//	t.Cleanup(func() { SetLiteralProvider(nil) })
//
//	want := "/tmp/working-dir"
//	SetLiteralProvider(func(id string, targetType reflect.Type) (any, bool, error) {
//		if id == "WorkingDir" && targetType == reflect.TypeOf("") {
//			return want, true, nil
//		}
//		return nil, false, nil
//	})
//
//	cfg := &Config{}
//	c := &Container{
//		registeredBeans:    map[string]bean{},
//		requiredDependency: map[string]reflect.Type{},
//	}
//
//	// Discover dependencies for the Config bean (should include "WorkingDir": string).
//	has, deps := c.checkForDependency(reflect.TypeOf(cfg))
//	require.True(t, has)
//	require.Equal(t, 1, len(deps))
//	require.Equal(t, "WorkingDir", deps[0])
//	require.Equal(t, reflect.TypeOf(""), c.requiredDependency["WorkingDir"])
//
//	// Register the Config bean with its dependencies but do not register "WorkingDir" bean.
//	c.registeredBeans["ServiceBeanConfig"] = bean{
//		id:              "ServiceBeanConfig",
//		instance:        cfg,
//		beanType:        reflect.TypeOf(cfg),
//		hasDependencies: true,
//		dependencies:    deps,
//	}
//
//	err := c.injectDependencies()
//	require.NoError(t, err)
//
//	// Verify literal was injected and a synthetic bean was created/cached.
//	require.Equal(t, want, cfg.WorkingDir)
//	synth, ok := c.registeredBeans["WorkingDir"]
//	require.True(t, ok)
//	require.Equal(t, want, synth.instance)
//	require.Equal(t, reflect.TypeOf(""), synth.beanType)
//}

func TestLiteralProvider_NotInstalled_MissingDependency(t *testing.T) {
	t.Cleanup(func() { SetLiteralProvider(nil) })
	SetLiteralProvider(nil)

	cfg := &Config{}
	c := &Container{
		registeredBeans:    map[string]bean{},
		requiredDependency: map[string]reflect.Type{},
	}
	_, deps := c.checkForDependency(reflect.TypeOf(cfg))

	c.registeredBeans["servicebeanconfig"] = bean{
		id:              "servicebeanconfig",
		instance:        cfg,
		beanType:        reflect.TypeOf(cfg),
		hasDependencies: true,
		dependencies:    deps,
	}

	err := c.injectDependencies()
	require.Error(t, err)
	require.Contains(t, err.Error(), "dependency bean 'workingdir' for 'servicebeanconfig' receiver bean not found")
}

func TestLiteralProvider_FoundFalse_MissingDependency(t *testing.T) {
	t.Cleanup(func() { SetLiteralProvider(nil) })
	SetLiteralProvider(func(id string, targetType reflect.Type) (any, bool, error) {
		return nil, false, nil // deliberately not found
	})

	cfg := &Config{}
	c := &Container{
		registeredBeans:    map[string]bean{},
		requiredDependency: map[string]reflect.Type{},
	}
	_, deps := c.checkForDependency(reflect.TypeOf(cfg))

	c.registeredBeans["servicebeanconfig"] = bean{
		id:              "servicebeanconfig",
		instance:        cfg,
		beanType:        reflect.TypeOf(cfg),
		hasDependencies: true,
		dependencies:    deps,
	}

	err := c.injectDependencies()
	require.Error(t, err)
	require.Contains(t, err.Error(), "dependency bean 'workingdir' for 'servicebeanconfig' receiver bean not found")
}

func TestLiteralProvider_ErrorPropagates(t *testing.T) {
	t.Cleanup(func() { SetLiteralProvider(nil) })
	SetLiteralProvider(func(id string, targetType reflect.Type) (any, bool, error) {
		return nil, false, errors.New("boom")
	})

	cfg := &Config{}
	c := &Container{
		registeredBeans:    map[string]bean{},
		requiredDependency: map[string]reflect.Type{},
	}
	_, deps := c.checkForDependency(reflect.TypeOf(cfg))

	c.registeredBeans["servicebeanconfig"] = bean{
		id:              "servicebeanconfig",
		instance:        cfg,
		beanType:        reflect.TypeOf(cfg),
		hasDependencies: true,
		dependencies:    deps,
	}

	err := c.injectDependencies()
	require.Error(t, err)
	require.Contains(t, err.Error(), "literal provider error for 'workingdir'")
	require.Contains(t, err.Error(), "boom")
}

func TestLiteralProvider_NotCalled_WhenBeanAlreadyExists(t *testing.T) {
	t.Cleanup(func() { SetLiteralProvider(nil) })
	SetLiteralProvider(func(id string, targetType reflect.Type) (any, bool, error) {
		t.Fatalf("literal provider should not be called when a bean exists")
		return nil, false, nil
	})

	cfg := &Config{}
	c := &Container{
		registeredBeans:    map[string]bean{},
		requiredDependency: map[string]reflect.Type{},
	}
	_, deps := c.checkForDependency(reflect.TypeOf(cfg))

	// Pre-register a real bean for "WorkingDir"
	c.registeredBeans["workingdir"] = bean{
		id:       "workingdir",
		instance: "/var/app",
		beanType: reflect.TypeOf(""),
	}

	c.registeredBeans["servicebeanconfig"] = bean{
		id:              "servicebeanconfig",
		instance:        cfg,
		beanType:        reflect.TypeOf(cfg),
		hasDependencies: true,
		dependencies:    deps,
	}

	err := c.injectDependencies()
	require.NoError(t, err)
	require.Equal(t, "/var/app", cfg.WorkingDir)
}

func TestLiteralProvider_NotUsed_ForNonStringDependencies(t *testing.T) {
	t.Cleanup(func() { SetLiteralProvider(nil) })
	SetLiteralProvider(func(id string, targetType reflect.Type) (any, bool, error) {
		// If this gets called for non-string types, that's a bug.
		t.Fatalf("literal provider should not be used for non-string dependencies: id=%s, type=%v", id, targetType)
		return nil, false, nil
	})

	svc := &Service{}
	c := &Container{
		registeredBeans:    map[string]bean{},
		requiredDependency: map[string]reflect.Type{},
	}
	// Discover dependencies for Service (pointer-to-structs only).
	has, deps := c.checkForDependency(reflect.TypeOf(svc))
	require.True(t, has)
	require.ElementsMatch(t, []string{"servicebeanconfig", "servicebeanlogger"}, deps)

	c.registeredBeans["servicebean"] = bean{
		id:              "servicebean",
		instance:        svc,
		beanType:        reflect.TypeOf(svc),
		hasDependencies: true,
		dependencies:    deps,
	}

	// No beans for "ServiceBeanConfig" or "ServiceBeanLogger" are registered; expect missing-bean error,
	// not a call to the literal provider.
	err := c.injectDependencies()
	require.Error(t, err)
	require.Contains(t, err.Error(), "dependency bean 'servicebeanconfig' for 'servicebean' receiver bean not found")
}

func TestLiteralProvider_InjectsStringIntoConfig(t *testing.T) {
	t.Cleanup(func() { SetLiteralProvider(nil) })

	// Arrange
	c := New()
	err := c.Register("ReceiverBean", reflect.TypeOf((*Service)(nil)))
	require.NoError(t, err)

	err = c.RegisterInstance("ServiceBeanConfig", &Config{})
	require.NoError(t, err)
	err = c.RegisterInstance("ServiceBeanLogger", &Logger{})
	require.NoError(t, err)

	// Provide a WorkingDir via literal provider.
	SetLiteralProvider(func(id string, typ reflect.Type) (any, bool, error) {
		if id == "workingdir" && typ.Kind() == reflect.String {
			return "/workspace", true, nil
		}
		return nil, false, nil
	})

	// Act
	err = c.Build()

	// Assert
	require.NoError(t, err)
	svcBean, ok := c.registeredBeans["receiverbean"]
	require.True(t, ok)
	svc, ok := svcBean.instance.(*Service)
	require.True(t, ok)
	require.NotNil(t, svc.Config)
	require.Equal(t, "/workspace", svc.Config.WorkingDir)
}

// --- DFS Cycle Detection Tests ---

// Two-node cycle: A -> B -> A
type cycleA struct {
	B *cycleB `di.inject:"B"`
}
type cycleB struct {
	A *cycleA `di.inject:"A"`
}

func TestCycleDetection_TwoNode(t *testing.T) {
	c := New()

	require.NoError(t, c.Register("A", reflect.TypeOf((*cycleA)(nil))))
	require.NoError(t, c.Register("B", reflect.TypeOf((*cycleB)(nil))))

	err := c.Build()
	require.Error(t, err)

	msg := err.Error()
	require.Contains(t, msg, "dependency cycle detected:")

	// Accept either traversal depending on map iteration order
	acceptable := []string{
		"a -> b -> a",
		"b -> a -> b",
	}
	require.True(t, containsAny(msg, acceptable), "error path was %q; expected one of %v", msg, acceptable)
}

// Three-node cycle: A -> B -> C -> A (order may rotate depending on traversal)
type cycleA3 struct {
	B *cycleB3 `di.inject:"B3"`
}
type cycleB3 struct {
	C *cycleC3 `di.inject:"C3"`
}
type cycleC3 struct {
	A *cycleA3 `di.inject:"A3"`
}

func TestCycleDetection_ThreeNode(t *testing.T) {
	c := New()

	require.NoError(t, c.Register("A3", reflect.TypeOf((*cycleA3)(nil))))
	require.NoError(t, c.Register("B3", reflect.TypeOf((*cycleB3)(nil))))
	require.NoError(t, c.Register("C3", reflect.TypeOf((*cycleC3)(nil))))

	err := c.Build()
	require.Error(t, err)

	msg := err.Error()
	require.Contains(t, msg, "dependency cycle detected:")

	acceptable := []string{
		"a3 -> b3 -> c3 -> a3",
		"b3 -> c3 -> a3 -> b3",
		"c3 -> a3 -> b3 -> c3",
	}
	require.True(t, containsAny(msg, acceptable), "error path was %q; expected one of %v", msg, acceptable)
}

// Self-cycle: A -> A
type selfCycleA struct {
	A *selfCycleA `di.inject:"Aself"`
}

func TestCycleDetection_SelfCycle(t *testing.T) {
	c := New()

	require.NoError(t, c.Register("Aself", reflect.TypeOf((*selfCycleA)(nil))))

	err := c.Build()
	require.Error(t, err)

	msg := err.Error()
	require.Contains(t, msg, "dependency cycle detected:")
	// Path should show a direct loop
	require.True(t, containsAny(msg, []string{"aself -> aself"}), "error path was %q; expected %q", msg, "aself -> aself")
}

// Helper: returns true if s contains any of the needles
func containsAny(s string, needles []string) bool {
	for _, n := range needles {
		if strings.Contains(s, n) {
			return true
		}
	}
	return false
}

// Resolve/ResolveSafe tests implemented as suite methods to match the pattern in container_test.go.

func (suite *TestSuite) TestResolveSafe_ReturnsErrorOnEmptyID() {
	c := New()
	v, err := c.ResolveSafe("")
	assert.Nil(suite.T(), v)
	require.Error(suite.T(), err)
	require.Equal(suite.T(), ErrBeanIdParamIsEmpty, err)
}

func (suite *TestSuite) TestResolveSafe_BuildsIfNeededAndReturnsInstance() {
	c := New()
	require.NoError(suite.T(), c.RegisterInstance("WorkingDir", "/tmp"))
	// No explicit Build call; ResolveSafe should trigger it.

	v, err := c.ResolveSafe("WorkingDir")
	require.NoError(suite.T(), err)
	require.Equal(suite.T(), "/tmp", v)
}

func (suite *TestSuite) TestResolveSafe_NotFound() {
	c := New()
	// Trigger build (implicitly) and attempt to resolve a missing bean.
	_, err := c.ResolveSafe("NoSuchBean")
	require.Error(suite.T(), err)
	require.Contains(suite.T(), err.Error(), "not found")
}

func (suite *TestSuite) TestResolve_ReturnsInstance() {
	c := New()
	require.NoError(suite.T(), c.RegisterInstance("ServiceBeanLogger", &Logger{}))

	v := c.Resolve("ServiceBeanLogger")
	_, ok := v.(*Logger)
	require.True(suite.T(), ok, "expected *Logger, got %T", v)
}

func (suite *TestSuite) TestResolve_PanicsWhenNotFound() {
	c := New()
	require.Panics(suite.T(), func() {
		_ = c.Resolve("NoSuchBean")
	})
}

func (suite *TestSuite) TestResolveSafe_IdempotentBuildAndSingleton() {
	c := New()

	// Register a service and its dependencies to ensure Build creates and injects.
	require.NoError(suite.T(), c.Register("ServiceBean", reflect.TypeOf((*Service)(nil))))
	require.NoError(suite.T(), c.RegisterInstance("ServiceBeanConfig", &Config{WorkingDir: "/app"}))
	require.NoError(suite.T(), c.RegisterInstance("ServiceBeanLogger", &Logger{}))
	// Provide the literal dependency explicitly to avoid missing-bean error during injection.
	require.NoError(suite.T(), c.RegisterInstance("WorkingDir", "/app"))
	// No explicit Build here; we rely on ResolveSafe to build once.

	// First resolve should build the container and return the instance.
	v1, err := c.ResolveSafe("ServiceBean")
	require.NoError(suite.T(), err)
	svc1, ok := v1.(*Service)
	require.True(suite.T(), ok)
	require.NotNil(suite.T(), svc1.Config)
	require.Equal(suite.T(), "/app", svc1.Config.WorkingDir)
	require.NotNil(suite.T(), svc1.Logger)

	// Second resolve should return the same singleton instance.
	v2, err := c.ResolveSafe("ServiceBean")
	require.NoError(suite.T(), err)
	svc2, ok := v2.(*Service)
	require.True(suite.T(), ok)
	require.Same(suite.T(), svc1, svc2)
}

// --- Additional coverage and failure-path tests ---

// --- ResolveAs generic helper tests ---

func TestResolveAs_Success_PtrStruct(t *testing.T) {
	c := New()
	require.NoError(t, c.RegisterInstance("ServiceBeanLogger", &Logger{}))

	got, err := ResolveAs[*Logger](c, "ServiceBeanLogger")
	require.NoError(t, err)
	require.NotNil(t, got)
}

func TestResolveAs_Success_StringLiteral(t *testing.T) {
	c := New()
	require.NoError(t, c.RegisterInstance("WorkingDir", "/tmp"))

	got, err := ResolveAs[string](c, "WorkingDir")
	require.NoError(t, err)
	require.Equal(t, "/tmp", got)
}

func TestResolveAs_WrongRequestedType(t *testing.T) {
	c := New()
	require.NoError(t, c.RegisterInstance("ServiceBeanLogger", &Logger{}))

	_, err := ResolveAs[*Service](c, "ServiceBeanLogger")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not of requested type")
}

func TestResolveAs_NotFound(t *testing.T) {
	c := New()

	_, err := ResolveAs[*Logger](c, "NoSuchBean")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestResolveAs_EmptyID(t *testing.T) {
	c := New()

	_, err := ResolveAs[*Logger](c, "")
	require.Error(t, err)
	require.Equal(t, ErrBeanIdParamIsEmpty, err)
}

// --- Initializer interface tests ---

type initCfg struct {
	Dir string `di.inject:"WorkingDir"`
}

type initBean struct {
	Cfg    *initCfg `di.inject:"InitCfg"`
	Inited bool
}

func (b *initBean) Initialize() error {
	if b.Cfg == nil || b.Cfg.Dir == "" {
		return errors.New("not ready")
	}
	b.Inited = true
	return nil
}

func TestInitializer_CalledAfterInjection(t *testing.T) {
	c := New()

	// Register types
	require.NoError(t, c.Register("InitBean", reflect.TypeOf((*initBean)(nil))))
	require.NoError(t, c.Register("InitCfg", reflect.TypeOf((*initCfg)(nil))))
	// Provide literal dependency
	require.NoError(t, c.RegisterInstance("WorkingDir", "/data"))

	// Build
	require.NoError(t, c.Build())

	// Verify Initialized
	b, ok := c.registeredBeans["initbean"]
	require.True(t, ok)
	ib, ok := b.instance.(*initBean)
	require.True(t, ok)
	require.True(t, ib.Inited, "initializer should have run and set Inited=true")
}

// --- Initialization order tests ---

var initOrder []string

type orderB struct{}

func (b *orderB) Initialize() error {
	initOrder = append(initOrder, "B")
	return nil
}

type orderA struct {
	B *orderB `di.inject:"OrderB"`
}

func (a *orderA) Initialize() error {
	// B must have been initialized already
	if len(initOrder) == 0 || initOrder[len(initOrder)-1] != "B" {
		return errors.New("order violation: B must initialize before A")
	}
	initOrder = append(initOrder, "A")
	return nil
}

type orderC struct {
	A *orderA `di.inject:"OrderA"`
}

func (c3 *orderC) Initialize() error {
	// A must have been initialized already, and hence B before A
	if len(initOrder) < 2 || initOrder[len(initOrder)-1] != "A" {
		return errors.New("order violation: A must initialize before C")
	}
	initOrder = append(initOrder, "C")
	return nil
}

func TestInitializer_OrderByDependencies_Depth2(t *testing.T) {
	initOrder = nil
	c := New()
	require.NoError(t, c.Register("OrderA", reflect.TypeOf((*orderA)(nil))))
	require.NoError(t, c.Register("OrderB", reflect.TypeOf((*orderB)(nil))))

	require.NoError(t, c.Build())
	// Expect B before A
	require.Equal(t, []string{"B", "A"}, initOrder)
}

func TestInitializer_OrderByDependencies_Depth3(t *testing.T) {
	initOrder = nil
	c := New()
	require.NoError(t, c.Register("OrderC", reflect.TypeOf((*orderC)(nil))))
	require.NoError(t, c.Register("OrderA", reflect.TypeOf((*orderA)(nil))))
	require.NoError(t, c.Register("OrderB", reflect.TypeOf((*orderB)(nil))))

	require.NoError(t, c.Build())
	// Expected order: B -> A -> C
	require.Equal(t, []string{"B", "A", "C"}, initOrder)
}
