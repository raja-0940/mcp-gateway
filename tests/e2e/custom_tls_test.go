//go:build e2e

package e2e

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"time"

	mcpv1alpha1 "github.com/Kuadrant/mcp-gateway/api/v1alpha1"
	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	tlsServerName     = "mcp-tls-server"
	tlsServerPort     = int32(8443)
	tlsListenerName   = "mcps-https"
	tlsListenerHost   = "*.mcp-tls.local"
	tlsServerHostname = "tls-server.mcp-tls.local"
	caKeypairSecret   = "private-ca-keypair"
	certManagerNS     = "cert-manager"
	caLabeledSecret   = "e2e-ca-bundle"
	wrongCaSecret     = "e2e-wrong-ca"

	githubMCPHost = "api.githubcopilot.com"
	githubMCPPort = int32(443)
	githubMCPPath = "/mcp"

	// hairpin TLS test constants
	hairpinListenerName   = "mcps-hairpin"
	hairpinListenerPort   = 8443
	hairpinNodePort       = 30443
	hairpinHostPort       = 8009
	hairpinPublicHost     = "hairpin-tls.mcp-tls.local"
	hairpinGatewayCertSec = "mcp-gateway-hairpin-cert"
	hairpinCACertSecret   = "hairpin-ca-bundle"
	hairpinExtName        = "hairpin-tls-ext"
	hairpinDeploymentName = "mcp-gateway" // operator uses this fixed name
)

var _ = Describe("Custom TLS Configuration", Ordered, func() {
	var (
		testResources    []client.Object
		mcpGatewayClient *NotifyingMCPClient
	)

	BeforeAll(func() {
		By("Checking cert-manager is installed")
		probe := &unstructured.UnstructuredList{}
		probe.SetGroupVersionKind(schema.GroupVersionKind{
			Group: "cert-manager.io", Version: "v1", Kind: "ClusterIssuerList",
		})
		if err := k8sClient.List(ctx, probe); err != nil {
			Skip("cert-manager not installed - skipping Custom TLS tests")
		}

		By("Checking TLS test server is deployed")
		deploy := &appsv1.Deployment{}
		if err := k8sClient.Get(ctx, types.NamespacedName{
			Name: tlsServerName, Namespace: TestServerNameSpace,
		}, deploy); err != nil {
			Skip("TLS test server not deployed (run 'make deploy-tls-test-server') - skipping Custom TLS tests")
		}
	})

	BeforeEach(func() {
		testResources = []client.Object{}
		Eventually(func(g Gomega) {
			var err error
			mcpGatewayClient, err = NewMCPGatewayClientWithNotifications(ctx, gatewayURL, nil)
			g.Expect(err).NotTo(HaveOccurred())
		}, TestTimeoutMedium, TestRetryInterval).Should(Succeed())
	})

	AfterEach(func() {
		if mcpGatewayClient != nil {
			_ = mcpGatewayClient.Close()
			mcpGatewayClient = nil
		}
		for _, obj := range testResources {
			CleanupResource(ctx, k8sClient, obj)
		}
		testResources = []client.Object{}
	})

	It("[HTTPS] [Happy] broker connects to TLS upstream with custom CA certificate", func() {
		By("Extracting CA cert from cert-manager secret")
		caSecret := &corev1.Secret{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: caKeypairSecret, Namespace: certManagerNS,
		}, caSecret)).To(Succeed())
		caCertPEM, ok := caSecret.Data["ca.crt"]
		Expect(ok).To(BeTrue(), "cert-manager CA secret should have ca.crt key")
		Expect(caCertPEM).NotTo(BeEmpty())

		By("Creating labeled CA secret in test namespace")
		labeledCA := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      caLabeledSecret,
				Namespace: TestServerNameSpace,
				Labels: map[string]string{
					"mcp.kuadrant.io/secret": "true",
					"e2e":                    "test",
				},
			},
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{
				"ca.crt": caCertPEM,
			},
		}
		_ = k8sClient.Delete(ctx, labeledCA)
		Expect(k8sClient.Create(ctx, labeledCA)).To(Succeed())
		testResources = append(testResources, labeledCA)

		By("Creating MCPServerRegistration with caCertSecretRef targeting the TLS server")
		registration := NewTestResources("custom-tls", k8sClient).
			ForInternalService(tlsServerName, tlsServerPort).
			WithHostname(tlsServerHostname).
			WithPrefix("tls_e2e_").
			WithSectionName(tlsListenerName).
			WithCACertSecretRef(caLabeledSecret, "ca.crt").
			Build()
		testResources = append(testResources, registration.GetObjects()...)
		registeredServer := registration.Register(ctx)

		By("Verifying MCPServerRegistration becomes ready")
		Eventually(func(g Gomega) {
			g.Expect(VerifyMCPServerRegistrationReady(ctx, k8sClient, registeredServer.Name, registeredServer.Namespace)).To(Succeed())
		}, TestTimeoutConfigSync, TestRetryInterval).Should(Succeed())

		By("Verifying tools with tls_e2e_ prefix are present")
		Eventually(func(g Gomega) {
			toolsList, err := mcpGatewayClient.ListTools(ctx, mcpgo.ListToolsRequest{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(toolsList).NotTo(BeNil())
			g.Expect(verifyMCPServerRegistrationToolsPresent("tls_e2e_", toolsList)).To(BeTrue(),
				"tools with prefix tls_e2e_ should exist")
		}, TestTimeoutConfigSync, TestRetryInterval).Should(Succeed())
	})

	It("[HTTPS] [Negative] broker rejects TLS upstream with wrong CA certificate", func() {
		By("Generating a wrong CA certificate")
		wrongCAPEM := generateSelfSignedCACert()

		By("Creating labeled secret with wrong CA")
		wrongCA := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      wrongCaSecret,
				Namespace: TestServerNameSpace,
				Labels: map[string]string{
					"mcp.kuadrant.io/secret": "true",
					"e2e":                    "test",
				},
			},
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{
				"ca.crt": wrongCAPEM,
			},
		}
		_ = k8sClient.Delete(ctx, wrongCA)
		Expect(k8sClient.Create(ctx, wrongCA)).To(Succeed())
		testResources = append(testResources, wrongCA)

		By("Creating MCPServerRegistration with wrong CA")
		registration := NewTestResources("wrong-tls", k8sClient).
			ForInternalService(tlsServerName, tlsServerPort).
			WithHostname(tlsServerHostname).
			WithPrefix("tls_wrong_").
			WithSectionName(tlsListenerName).
			WithCACertSecretRef(wrongCaSecret, "ca.crt").
			Build()
		testResources = append(testResources, registration.GetObjects()...)
		registeredServer := registration.Register(ctx)

		By("Verifying MCPServerRegistration is not ready with certificate error")
		Eventually(func(g Gomega) {
			mcpsr := &mcpv1alpha1.MCPServerRegistration{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: registeredServer.Name, Namespace: registeredServer.Namespace,
			}, mcpsr)).To(Succeed())
			g.Expect(mcpsr.Status.Conditions).NotTo(BeEmpty())
			for _, cond := range mcpsr.Status.Conditions {
				if cond.Type == "Ready" {
					g.Expect(cond.Status).To(Equal(metav1.ConditionFalse),
						"MCPServerRegistration should not be ready with wrong CA")
					g.Expect(cond.Message).To(ContainSubstring("x509"),
						"condition message should indicate a TLS certificate error")
					return
				}
			}
			g.Expect(false).To(BeTrue(), "no Ready condition found")
		}, TestTimeoutConfigSync, TestRetryInterval).Should(Succeed())

		By("Verifying tools with tls_wrong_ prefix are absent")
		Eventually(func(g Gomega) {
			toolsList, err := mcpGatewayClient.ListTools(ctx, mcpgo.ListToolsRequest{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(verifyMCPServerRegistrationToolsPresent("tls_wrong_", toolsList)).To(BeFalse(),
				"tools with prefix tls_wrong_ should NOT exist")
		}, TestTimeoutMedium, TestRetryInterval).Should(Succeed())
	})
})

var _ = Describe("HTTPS External Backends", func() {
	var testResources []client.Object

	AfterEach(func() {
		for _, obj := range testResources {
			CleanupResource(ctx, k8sClient, obj)
		}
		testResources = nil
	})

	It("[HTTPS] [HTTPS_EXTERNAL] External GitHub MCP server discovers tools over public TLS", func() {
		pat := os.Getenv("GITHUB_MCP_PAT")
		if pat == "" {
			Skip("GITHUB_MCP_PAT not set — skipping external GitHub MCP test")
		}

		By("Creating a Secret containing the GitHub PAT")
		patSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      UniqueName("github-pat"),
				Namespace: TestServerNameSpace,
				Labels: map[string]string{
					"mcp.kuadrant.io/secret": "true",
					"e2e":                    "test",
				},
			},
			Type: corev1.SecretTypeOpaque,
			StringData: map[string]string{
				"token": fmt.Sprintf("Bearer %s", pat),
			},
		}

		By("Registering the GitHub MCP server as an external hostname backend")
		resources := NewTestResources("github-mcp", k8sClient).
			ForExternalService(githubMCPHost, githubMCPPort).
			WithPrefix("github_").
			WithPath(githubMCPPath).
			WithCredential(patSecret, "token").
			WithParentGateway(GatewayName, GatewayNamespace).
			Build()
		testResources = append(testResources, resources.GetObjects()...)
		for _, obj := range resources.GetObjects() {
			CleanupResource(ctx, k8sClient, obj)
		}
		resources.Register(ctx)

		mcpServer := resources.GetMCPServer()

		By("Waiting for MCPServerRegistration to become Ready")
		Eventually(func(g Gomega) {
			g.Expect(VerifyMCPServerRegistrationReady(ctx, k8sClient, mcpServer.Name, TestServerNameSpace)).To(Succeed())
		}, TestTimeoutLong, TestRetryInterval).Should(Succeed())

		By("Asserting the registered server has discovered at least one tool")
		Eventually(func(g Gomega) {
			sr := &mcpv1alpha1.MCPServerRegistration{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(mcpServer), sr)).To(Succeed())
			g.Expect(sr.Status.DiscoveredTools).To(BeNumerically(">", 0),
				"expected at least one tool discovered over HTTPS from GitHub MCP")
		}, TestTimeoutLong, TestRetryInterval).Should(Succeed())

		By("Asserting the config stored for this server uses an https:// URL")
		Eventually(func(g Gomega) {
			secret := &corev1.Secret{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKey{
				Name:      ConfigMapName,
				Namespace: SystemNamespace,
			}, secret)).To(Succeed())
			configData, ok := secret.Data["config.yaml"]
			g.Expect(ok).To(BeTrue(), "config secret should have config.yaml key")
			configStr := string(configData)
			g.Expect(configStr).To(ContainSubstring(githubMCPHost),
				"expected to find GitHub MCP host in config")
			g.Expect(configStr).To(ContainSubstring("https://"),
				"GitHub MCP server should have an https:// URL in config")
		}, TestTimeoutMedium, TestRetryInterval).Should(Succeed())
	})

	It("[HTTPS] [HTTPS_EXTERNAL] In-cluster MCP server accessible over public TLS via real certs", func() {
		if os.Getenv("E2E_HTTPS_REAL_CERTS") != "true" {
			Skip("Skipping: E2E_HTTPS_REAL_CERTS is not set to 'true'. " +
				"This test requires a cluster with a real wildcard certificate.")
		}
		if e2eScheme != "https" {
			Skip("Skipping: E2E_SCHEME must be 'https' for real-cert tests")
		}

		By("Registering an internal MCP server via HTTPS gateway")
		resources := NewTestResources("https-real-certs", k8sClient).
			ForInternalService("mcp-test-server1", 9090).
			WithPrefix("realcert_").
			WithParentGateway(GatewayName, GatewayNamespace).
			Build()
		testResources = append(testResources, resources.GetObjects()...)
		for _, obj := range resources.GetObjects() {
			CleanupResource(ctx, k8sClient, obj)
		}
		resources.Register(ctx)

		mcpServer := resources.GetMCPServer()

		By("Waiting for MCPServerRegistration to become Ready over HTTPS")
		Eventually(func(g Gomega) {
			g.Expect(VerifyMCPServerRegistrationReady(ctx, k8sClient, mcpServer.Name, TestServerNameSpace)).To(Succeed())
		}, TestTimeoutLong, TestRetryInterval).Should(Succeed())

		By("Verifying tools are accessible via the HTTPS gateway URL")
		var mcpClient *NotifyingMCPClient
		Eventually(func(g Gomega) {
			var err error
			mcpClient, err = NewMCPGatewayClientWithNotifications(ctx, gatewayURL, nil)
			g.Expect(err).NotTo(HaveOccurred())
		}, TestTimeoutMedium, TestRetryInterval).Should(Succeed())
		defer func() { _ = mcpClient.Close() }()

		By("Verifying tools/list succeeds over HTTPS")
		Eventually(func(g Gomega) {
			toolsList, err := mcpClient.ListTools(ctx, mcpgo.ListToolsRequest{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(toolsList).NotTo(BeNil())
			g.Expect(verifyMCPServerRegistrationToolsPresent("realcert_", toolsList)).To(BeTrue(),
				"expected to find realcert_ prefixed tools over HTTPS")
		}, TestTimeoutLong, TestRetryInterval).Should(Succeed())
	})
})

var _ = Describe("HTTPS Hairpin TLS", Ordered, func() {
	// Tests the fix for #1130: when the gateway has an HTTPS listener, the
	// broker-router's hairpin initialize request must set ServerName on the TLS
	// config to verify the cert against the public hostname. The gateway already
	// has an mcps-hairpin HTTPS listener (config/istio/gateway/gateway.yaml)
	// with a cert issued by the private CA (config/test-servers/tls-server-cert-manager.yaml).

	BeforeAll(func() {
		By("Checking cert-manager is installed")
		probe := &unstructured.UnstructuredList{}
		probe.SetGroupVersionKind(schema.GroupVersionKind{
			Group: "cert-manager.io", Version: "v1", Kind: "ClusterIssuerList",
		})
		if err := k8sClient.List(ctx, probe); err != nil {
			Skip("cert-manager not installed - skipping HTTPS hairpin tests")
		}

		By("Checking the hairpin gateway cert secret exists")
		secret := &corev1.Secret{}
		if err := k8sClient.Get(ctx, types.NamespacedName{
			Name: hairpinGatewayCertSec, Namespace: GatewayNamespace,
		}, secret); err != nil {
			Skip("hairpin gateway cert not found - skipping (run 'make deploy-tls-test-server')")
		}
	})

	It("[HTTPS] [Hairpin] tools/call succeeds through HTTPS gateway listener with private CA", func() {
		By("Creating MCPGatewayExtension for the HTTPS listener")
		hairpinExt := NewMCPGatewayExtensionSetup(k8sClient).
			WithName(hairpinExtName).
			InNamespace(SystemNamespace).
			TargetingGateway(GatewayName, GatewayNamespace).
			WithSectionName(hairpinListenerName).
			WithPublicHost(hairpinPublicHost).
			WithListenerPort(int32(hairpinListenerPort)).
			Build()

		hairpinExt.Clean(ctx).Register(ctx)
		DeferCleanup(func() {
			hairpinExt.TearDown(ctx)
		})

		By("Waiting for MCPGatewayExtension to become ready")
		Eventually(func(g Gomega) {
			g.Expect(VerifyMCPGatewayExtensionReady(ctx, k8sClient, hairpinExtName, SystemNamespace)).To(Succeed())
		}, TestTimeoutLong, TestRetryInterval).Should(Succeed())

		By("Waiting for broker-router deployment to be ready")
		Eventually(func(g Gomega) {
			deploy := &appsv1.Deployment{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: hairpinDeploymentName, Namespace: SystemNamespace,
			}, deploy)).To(Succeed())
			g.Expect(deploy.Status.ReadyReplicas).To(BeNumerically(">=", 1))
		}, TestTimeoutLong, TestRetryInterval).Should(Succeed())

		By("Extracting CA cert from cert-manager and creating secret in system namespace")
		caSecret := &corev1.Secret{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: caKeypairSecret, Namespace: certManagerNS,
		}, caSecret)).To(Succeed())
		caCertPEM, ok := caSecret.Data["ca.crt"]
		Expect(ok).To(BeTrue(), "private-ca-keypair should have ca.crt")

		caBundle := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      hairpinCACertSecret,
				Namespace: SystemNamespace,
			},
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{
				"ca.crt": caCertPEM,
			},
		}
		_ = k8sClient.Delete(ctx, caBundle)
		Expect(k8sClient.Create(ctx, caBundle)).To(Succeed())
		DeferCleanup(func() {
			_ = k8sClient.Delete(context.Background(), caBundle)
		})

		By("Patching broker-router deployment: mount CA cert volume and add --gateway-ca-cert flag")
		combinedPatch := fmt.Sprintf(`[`+
			`{"op":"add","path":"/spec/template/spec/volumes/-","value":{"name":"gateway-ca","secret":{"secretName":"%s"}}},`+
			`{"op":"add","path":"/spec/template/spec/containers/0/volumeMounts/-","value":{"name":"gateway-ca","mountPath":"/certs/gateway-ca.crt","subPath":"ca.crt","readOnly":true}},`+
			`{"op":"add","path":"/spec/template/spec/containers/0/command/-","value":"--gateway-ca-cert=/certs/gateway-ca.crt"}`+
			`]`, hairpinCACertSecret)
		Expect(PatchDeploymentJSON(ctx, SystemNamespace, hairpinDeploymentName, combinedPatch)).To(Succeed())

		DeferCleanup(func() {
			cleanupCtx := context.Background()
			_ = RemoveDeploymentCommandFlag(cleanupCtx, SystemNamespace, hairpinDeploymentName, "--gateway-ca-cert=/certs/gateway-ca.crt")
			_ = RemoveDeploymentVolumeMount(cleanupCtx, SystemNamespace, hairpinDeploymentName, "gateway-ca")
			_ = RemoveDeploymentVolume(cleanupCtx, SystemNamespace, hairpinDeploymentName, "gateway-ca")
		})

		Expect(WaitForDeploymentReady(ctx, SystemNamespace, hairpinDeploymentName)).To(Succeed())

		By("Registering an MCP server on the HTTPS listener")
		registration := NewTestResources("hairpin-tls", k8sClient).
			ForInternalService("mcp-test-server1", 9090).
			WithHostname(hairpinPublicHost).
			WithPrefix("hairpin_").
			WithSectionName(hairpinListenerName).
			Build()
		for _, obj := range registration.GetObjects() {
			CleanupResource(ctx, k8sClient, obj)
		}
		registeredServer := registration.Register(ctx)
		DeferCleanup(func() {
			for _, obj := range registration.GetObjects() {
				CleanupResource(context.Background(), k8sClient, obj)
			}
		})

		By("Verifying MCPServerRegistration becomes ready")
		Eventually(func(g Gomega) {
			g.Expect(VerifyMCPServerRegistrationReady(ctx, k8sClient, registeredServer.Name, registeredServer.Namespace)).To(Succeed())
		}, TestTimeoutConfigSync, TestRetryInterval).Should(Succeed())

		By("Connecting to the gateway via HTTPS with the private CA cert")
		pool := x509.NewCertPool()
		Expect(pool.AppendCertsFromPEM(caCertPEM)).To(BeTrue())

		httpClient := &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					RootCAs:    pool,
					MinVersion: tls.VersionTLS12,
				},
				// route hairpinPublicHost to localhost via custom dialer
				DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
					host, _, _ := net.SplitHostPort(addr)
					if host == hairpinPublicHost {
						addr = fmt.Sprintf("localhost:%d", hairpinHostPort)
					}
					return (&net.Dialer{}).DialContext(ctx, network, addr)
				},
			},
		}

		gatewayHTTPSURL := "https://" + net.JoinHostPort(hairpinPublicHost, fmt.Sprintf("%d", hairpinHostPort)) + "/mcp"
		var mcpClient *mcpclient.Client
		Eventually(func(g Gomega) {
			if mcpClient != nil {
				_ = mcpClient.Close()
				mcpClient = nil
			}
			var err error
			mcpClient, err = mcpclient.NewStreamableHttpClient(gatewayHTTPSURL,
				transport.WithHTTPHeaders(map[string]string{"e2e": "client"}),
				transport.WithHTTPBasicClient(httpClient),
			)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(mcpClient.Start(ctx)).To(Succeed())
			_, err = mcpClient.Initialize(ctx, mcpgo.InitializeRequest{
				Params: mcpgo.InitializeParams{
					ProtocolVersion: mcpgo.LATEST_PROTOCOL_VERSION,
					Capabilities:    mcpgo.ClientCapabilities{},
					ClientInfo:      mcpgo.Implementation{Name: "e2e-hairpin", Version: "0.0.1"},
				},
			})
			g.Expect(err).NotTo(HaveOccurred())
		}, TestTimeoutMedium, TestRetryInterval).Should(Succeed())
		defer func() { _ = mcpClient.Close() }()

		By("Verifying tools with hairpin_ prefix are discoverable")
		Eventually(func(g Gomega) {
			toolsList, err := mcpClient.ListTools(ctx, mcpgo.ListToolsRequest{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(toolsList).NotTo(BeNil())
			g.Expect(verifyMCPServerRegistrationToolsPresent("hairpin_", toolsList)).To(BeTrue(),
				"tools with prefix hairpin_ should exist")
		}, TestTimeoutConfigSync, TestRetryInterval).Should(Succeed())

		By("Calling a tool to trigger the hairpin init through the HTTPS listener")
		toolName := "hairpin_greet"
		res, err := mcpClient.CallTool(ctx, mcpgo.CallToolRequest{
			Params: mcpgo.CallToolParams{
				Name:      toolName,
				Arguments: map[string]string{"name": "hairpin-tls-test"},
			},
		})
		Expect(err).NotTo(HaveOccurred(), "tools/call should succeed — hairpin init through HTTPS must work")
		Expect(res).NotTo(BeNil())
		Expect(res.Content).NotTo(BeEmpty(), "tool call should return content")
	})
})

func generateSelfSignedCACert() []byte {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	Expect(err).NotTo(HaveOccurred())
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Wrong CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	Expect(err).NotTo(HaveOccurred())
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}
