package controllers

import (
	"context"
	"errors"
	"time"

	"github.com/cybozu-go/neco-tenant-controller/pkg/argocd"
	cacheclient "github.com/cybozu-go/neco-tenant-controller/pkg/client"
	tenantconfig "github.com/cybozu-go/neco-tenant-controller/pkg/config"
	"github.com/cybozu-go/neco-tenant-controller/pkg/constants"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func fillApplication(name, namespace, project string) (*unstructured.Unstructured, error) {
	app := argocd.Application()
	app.SetName(name)
	app.SetNamespace(namespace)
	err := unstructured.SetNestedField(app.UnstructuredContent(), project, "spec", "project")
	if err != nil {
		return nil, err
	}
	err = unstructured.SetNestedField(app.UnstructuredContent(), "https://github.com/neco-test/apps-sandbox.git", "spec", "source", "repoURL")
	if err != nil {
		return nil, err
	}
	err = unstructured.SetNestedMap(app.UnstructuredContent(), map[string]interface{}{}, "spec", "destination")
	if err != nil {
		return nil, err
	}
	return app, nil
}

var _ = Describe("Application controller", func() {
	ctx := context.Background()
	var stopFunc func()
	var config *tenantconfig.Config

	BeforeEach(func() {
		mgr, err := ctrl.NewManager(k8sCfg, ctrl.Options{
			Scheme:             scheme,
			LeaderElection:     false,
			MetricsBindAddress: "0",
			NewClient:          cacheclient.NewCachingClient,
		})
		Expect(err).ToNot(HaveOccurred())

		config = &tenantconfig.Config{
			Namespace: tenantconfig.NamespaceConfig{
				CommonLabels:        nil,
				GroupKey:            "team",
				RoleBindingTemplate: "",
			},
			ArgoCD: tenantconfig.ArgoCDConfig{
				Namespace:          "argocd",
				AppProjectTemplate: "",
			},
		}
		ar := NewApplicationReconciler(mgr.GetClient(), config)
		err = ar.SetupWithManager(ctx, mgr)
		Expect(err).ToNot(HaveOccurred())

		ctx, cancel := context.WithCancel(ctx)
		stopFunc = cancel
		go func() {
			err := mgr.Start(ctx)
			if err != nil {
				panic(err)
			}
		}()
		time.Sleep(100 * time.Millisecond)
	})

	AfterEach(func() {
		stopFunc()
		time.Sleep(100 * time.Millisecond)
	})

	It("should sync an application", func() {
		tenantApp, err := fillApplication("app", "sub-1", "a-team")
		tenantApp.SetLabels(map[string]string{
			"kubernetes.io/name": "app",
			"foo":                "bar",
		})
		tenantApp.SetAnnotations(map[string]string{
			"kubernetes.io/name": "app",
			"abc":                "def",
		})
		tenantApp.SetFinalizers([]string{
			"resources-finalizer.argocd.argoproj.io",
			"my.finalizer",
		})
		Expect(err).ToNot(HaveOccurred())

		By("syncing an application spec")
		err = k8sClient.Create(ctx, tenantApp)
		Expect(err).ToNot(HaveOccurred())

		argocdApp := argocd.Application()
		Eventually(func() error {
			if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: config.ArgoCD.Namespace, Name: tenantApp.GetName()}, argocdApp); err != nil {
				return err
			}
			return nil
		}).Should(Succeed())

		Expect(argocdApp.GetLabels()).Should(MatchAllKeys(Keys{
			constants.OwnerAppNamespace: Equal("sub-1"),
			"foo":                       Equal("bar"),
		}))
		Expect(argocdApp.GetLabels()).ShouldNot(HaveKey("kubernetes.io/name"))
		Expect(argocdApp.GetAnnotations()).Should(MatchAllKeys(Keys{
			"abc": Equal("def"),
		}))
		Expect(argocdApp.GetAnnotations()).ShouldNot(HaveKey("kubernetes.io/name"))
		Expect(argocdApp.GetFinalizers()).Should(ContainElement("resources-finalizer.argocd.argoproj.io"))
		Expect(argocdApp.GetFinalizers()).ShouldNot(ContainElement("my.finalizer"))
		Expect(argocdApp.UnstructuredContent()["spec"]).Should(Equal(tenantApp.UnstructuredContent()["spec"]))

		By("syncing an application status")
		err = unstructured.SetNestedField(argocdApp.UnstructuredContent(), "Healthy", "status", "health", "status")
		Expect(err).ToNot(HaveOccurred())
		err = unstructured.SetNestedField(argocdApp.UnstructuredContent(), "successfully synced", "status", "operationState", "message")
		Expect(err).ToNot(HaveOccurred())
		err = unstructured.SetNestedField(argocdApp.UnstructuredContent(), "Succeeded", "status", "operationState", "phase")
		Expect(err).ToNot(HaveOccurred())
		err = unstructured.SetNestedField(argocdApp.UnstructuredContent(), "abcdefg", "status", "operationState", "operation", "sync", "revision")
		Expect(err).ToNot(HaveOccurred())
		err = unstructured.SetNestedField(argocdApp.UnstructuredContent(), time.Now().UTC().Format(time.RFC3339), "status", "operationState", "startedAt")
		Expect(err).ToNot(HaveOccurred())
		err = k8sClient.Update(ctx, argocdApp)
		Expect(err).ToNot(HaveOccurred())

		Eventually(func() error {
			if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: tenantApp.GetNamespace(), Name: tenantApp.GetName()}, tenantApp); err != nil {
				return err
			}
			if tenantApp.UnstructuredContent()["status"] == nil {
				return errors.New("status is nil")
			}
			return nil
		}).Should(Succeed())
		Expect(tenantApp.UnstructuredContent()["status"]).Should(Equal(argocdApp.UnstructuredContent()["status"]))
	})

	It("should remove an application on unmanaged namespace", func() {
		tenantApp, err := fillApplication("unmanaged-app", "sub-2", "a-team")
		Expect(err).ToNot(HaveOccurred())

		err = k8sClient.Create(ctx, tenantApp)
		Expect(err).ToNot(HaveOccurred())

		argocdApp := argocd.Application()
		Eventually(func() error {
			if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: config.ArgoCD.Namespace, Name: tenantApp.GetName()}, argocdApp); err != nil {
				return err
			}
			return nil
		}).Should(Succeed())

		ns := &corev1.Namespace{}
		ns.Name = "sub-2"
		ns.Labels = map[string]string{}
		err = k8sClient.Update(ctx, ns)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func() error {
			err := k8sClient.Get(ctx, client.ObjectKey{Namespace: config.ArgoCD.Namespace, Name: tenantApp.GetName()}, argocdApp)
			if apierrors.IsNotFound(err) {
				return nil
			}
			if err != nil {
				return err
			}
			return errors.New("application still exists")
		}).Should(Succeed())
	})

	It("should fix project", func() {
		tenantApp, err := fillApplication("changed-app", "sub-3", "a-team")
		Expect(err).ToNot(HaveOccurred())

		err = k8sClient.Create(ctx, tenantApp)
		Expect(err).ToNot(HaveOccurred())

		argocdApp := argocd.Application()
		Eventually(func() error {
			if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: config.ArgoCD.Namespace, Name: tenantApp.GetName()}, argocdApp); err != nil {
				return err
			}
			return nil
		}).Should(Succeed())

		ns := &corev1.Namespace{}
		ns.Name = "sub-3"
		ns.Labels = map[string]string{
			"team": "b-team",
		}
		err = k8sClient.Update(ctx, ns)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func() error {
			if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: tenantApp.GetNamespace(), Name: tenantApp.GetName()}, tenantApp); err != nil {
				return err
			}
			project, found, err := unstructured.NestedString(tenantApp.UnstructuredContent(), "spec", "project")
			if err != nil {
				return err
			}
			if !found {
				return errors.New("spec.project not found")
			}
			if project != "b-team" {
				return errors.New("spec.project has not been fixed: " + project)
			}
			return nil
		}).Should(Succeed())

		Eventually(func() error {
			if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: config.ArgoCD.Namespace, Name: tenantApp.GetName()}, argocdApp); err != nil {
				return err
			}
			project, found, err := unstructured.NestedString(argocdApp.UnstructuredContent(), "spec", "project")
			if err != nil {
				return err
			}
			if !found {
				return errors.New("spec.project not found")
			}
			if project != "b-team" {
				return errors.New("spec.project has not been fixed: " + project)
			}
			return nil
		}).Should(Succeed())
	})

	It("should remove application", func() {
		tenantApp, err := fillApplication("removed-app", "sub-1", "a-team")
		tenantApp.SetFinalizers([]string{constants.Finalizer})
		Expect(err).ToNot(HaveOccurred())

		By("syncing an application spec")
		err = k8sClient.Create(ctx, tenantApp)
		Expect(err).ToNot(HaveOccurred())

		argocdApp := argocd.Application()
		Eventually(func() error {
			if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: config.ArgoCD.Namespace, Name: tenantApp.GetName()}, argocdApp); err != nil {
				return err
			}
			return nil
		}).Should(Succeed())

		By("deleting an application")
		err = k8sClient.Delete(ctx, tenantApp)
		Expect(err).ToNot(HaveOccurred())

		Eventually(func() error {
			err := k8sClient.Get(ctx, client.ObjectKey{Namespace: config.ArgoCD.Namespace, Name: tenantApp.GetName()}, argocdApp)
			if apierrors.IsNotFound(err) {
				return nil
			}
			if err != nil {
				return err
			}
			return errors.New("application still exists")
		}).Should(Succeed())

		Eventually(func() error {
			err := k8sClient.Get(ctx, client.ObjectKey{Namespace: tenantApp.GetNamespace(), Name: tenantApp.GetName()}, tenantApp)
			if apierrors.IsNotFound(err) {
				return nil
			}
			if err != nil {
				return err
			}
			return errors.New("application still exists")
		}).Should(Succeed())
	})

})
