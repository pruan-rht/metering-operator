package deploy

import (
	"context"
	"fmt"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (deploy *Deployer) uninstallNamespace() error {
	err := deploy.client.CoreV1().Namespaces().Delete(context.TODO(), deploy.config.Namespace, metav1.DeleteOptions{})
	if apierrors.IsNotFound(err) {
		deploy.logger.Warnf("The %s namespace doesn't exist", deploy.config.Namespace)
		return nil
	}
	if err != nil {
		return err
	}
	deploy.logger.Infof("Deleted the %s namespace", deploy.config.Namespace)

	return nil
}

func (deploy *Deployer) uninstallMeteringConfig() error {
	err := deploy.meteringClient.MeteringConfigs(deploy.config.Namespace).Delete(context.TODO(), deploy.config.MeteringConfig.Name, metav1.DeleteOptions{})
	if apierrors.IsNotFound(err) {
		deploy.logger.Warnf("The MeteringConfig resource doesn't exist")
		return nil
	}
	if err != nil {
		return err
	}
	deploy.logger.Infof("Deleted the MeteringConfig resource")

	return nil
}

func (deploy *Deployer) uninstallMeteringOperatorGroup() error {
	err := deploy.olmV1Client.OperatorGroups(deploy.config.Namespace).Delete(context.TODO(), deploy.config.Namespace, metav1.DeleteOptions{})
	if apierrors.IsNotFound(err) {
		deploy.logger.Warnf("The metering OperatorGroup resource does not exist")
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to delete the metering OperatorGroup resource: %v", err)
	}
	deploy.logger.Infof("Deleted the metering OperatorGroup resource")

	return nil
}

func (deploy *Deployer) uninstallMeteringSubscription() error {
	_, err := deploy.olmV1Alpha1Client.Subscriptions(deploy.config.Namespace).Get(context.TODO(), deploy.config.SubscriptionName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		deploy.logger.Warnf("The %s metering Subscription in the %s namespace does not exist", deploy.config.SubscriptionName, deploy.config.Namespace)
		return nil
	}
	if err != nil {
		return err
	}

	err = deploy.olmV1Alpha1Client.Subscriptions(deploy.config.Namespace).Delete(context.TODO(), deploy.config.SubscriptionName, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("failed to delete the %s metering Subscription in the %s namespace: %v", deploy.config.SubscriptionName, deploy.config.Namespace, err)
	}
	deploy.logger.Infof("Deleted the %s metering Subscription resource in the %s namespace", deploy.config.SubscriptionName, deploy.config.Namespace)

	return nil
}

func (deploy *Deployer) uninstallMeteringCSV() error {
	// attempt to query for the metering subscription as we don't have a way of knowing
	// what the CSV's name is beforehand without exposing more configurable flags.
	// in the case where the subscription resource does not already exist, exit early
	// and hope that the user is re-running the olm-uninstall command.
	sub, err := deploy.olmV1Alpha1Client.Subscriptions(deploy.config.Namespace).Get(context.TODO(), deploy.config.SubscriptionName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		deploy.logger.Warnf("The metering Subscription does not exist")
		return nil
	}
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}

	if sub.Status.CurrentCSV == "" {
		deploy.logger.Warnf("Failed to get the 'status.currentCSV' stored in the %s metering Subscription resource", deploy.config.SubscriptionName)
		return nil
	}

	csvName := sub.Status.CurrentCSV
	deploy.logger.Infof("Found an existing metering subscription, attempting to delete the %s CSV", csvName)

	csv, err := deploy.olmV1Alpha1Client.ClusterServiceVersions(deploy.config.Namespace).Get(context.TODO(), csvName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		deploy.logger.Warnf("The %s metering CSV resource does not exist", csvName)
		return nil
	}
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}

	err = deploy.olmV1Alpha1Client.ClusterServiceVersions(deploy.config.Namespace).Delete(context.TODO(), csv.Name, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("failed to delete the %s metering CSV resource: %v", csvName, err)
	}
	deploy.logger.Infof("Deleted the %s metering CSV resource in the %s namespace", csvName, deploy.config.Namespace)

	return nil
}

func (deploy *Deployer) uninstallMeteringResources() error {
	err := deploy.uninstallMeteringDeployment()
	if err != nil {
		return fmt.Errorf("failed to delete the metering service account: %v", err)
	}

	err = deploy.uninstallMeteringServiceAccount()
	if err != nil {
		return fmt.Errorf("failed to delete the metering service account: %v", err)
	}

	err = deploy.uninstallMeteringRole()
	if err != nil {
		return fmt.Errorf("failed to delete the metering role: %v", err)
	}

	err = deploy.uninstallMeteringRoleBinding()
	if err != nil {
		return fmt.Errorf("failed to delete the metering role binding: %v", err)
	}

	if deploy.config.DeleteCRB {
		err = deploy.uninstallMeteringClusterRole()
		if err != nil {
			return fmt.Errorf("failed to delete the metering cluster role: %v", err)
		}

		err = deploy.uninstallMeteringClusterRoleBinding()
		if err != nil {
			return fmt.Errorf("failed to delete the metering cluster role binding: %v", err)
		}
		err = deploy.uninstallReportingOperatorClusterRole()
		if err != nil {
			return fmt.Errorf("failed to delete the reporting-operator ClusterRole resources: %v", err)
		}

		err = deploy.uninstallReportingOperatorClusterRoleBinding()
		if err != nil {
			return fmt.Errorf("failed to delete the reporting-operator ClusterRoleBinding resources: %v", err)
		}
	} else {
		deploy.logger.Infof("Skipped deleting the metering cluster role resources")
	}

	if deploy.config.DeletePVCs {
		err = deploy.uninstallMeteringPVCs()
		if err != nil {
			return fmt.Errorf("failed to delete the metering PVCs: %v", err)
		}
	} else {
		deploy.logger.Infof("Skipped deleting the metering PVCs")
	}

	return nil
}

// uninstallMeteringPVCs queries the namespace where Metering is
// currently deployed and searches for any of the HDFS PVCs using
// the 'app=hdfs' label selector. Note: we currently spin up those
// PVCs as a volumeClaimTemplate in the datanode/namenode templates
// so that means the StatefulSets aren't setting any owner references
// and the metering-ansible-operator isn't reconciling those resources
// so we need to explicitly delete them during cleanup.
func (deploy *Deployer) uninstallMeteringPVCs() error {
	// Attempt to get a list of PVCs that match the hdfs or hive labels
	pvcs, err := deploy.client.CoreV1().PersistentVolumeClaims(deploy.config.Namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: "app=hdfs",
	})
	if err != nil {
		return fmt.Errorf("failed to list all the metering PVCs in the %s namespace: %v", deploy.config.Namespace, err)
	}
	if len(pvcs.Items) == 0 {
		deploy.logger.Warnf("The HDFS PVCs don't exist")
		return nil
	}

	for _, pvc := range pvcs.Items {
		err = deploy.client.CoreV1().PersistentVolumeClaims(deploy.config.Namespace).Delete(context.TODO(), pvc.Name, metav1.DeleteOptions{})
		if apierrors.IsNotFound(err) {
			deploy.logger.Warnf("The %s PVC does not exist", pvc.Name)
			continue
		}
		if err != nil {
			// TODO: we should be returning an array of errors instead of a single err
			return fmt.Errorf("failed to delete the %s PVC: %v", pvc.Name, err)
		}
		deploy.logger.Infof("Deleted the %s PVC in the %s namespace", pvc.Name, deploy.config.Namespace)
	}

	return nil
}

func (deploy *Deployer) uninstallMeteringDeployment() error {
	err := deploy.client.AppsV1().Deployments(deploy.config.Namespace).Delete(context.TODO(), deploy.config.OperatorResources.Deployment.Name, metav1.DeleteOptions{})
	if apierrors.IsNotFound(err) {
		deploy.logger.Warnf("The metering deployment doesn't exist")
		return nil
	}
	if err != nil {
		return err
	}
	deploy.logger.Infof("Deleted the metering deployment")

	return nil
}

func (deploy *Deployer) uninstallMeteringServiceAccount() error {
	err := deploy.client.CoreV1().ServiceAccounts(deploy.config.Namespace).Delete(context.TODO(), deploy.config.OperatorResources.ServiceAccount.Name, metav1.DeleteOptions{})
	if apierrors.IsNotFound(err) {
		deploy.logger.Warnf("The metering service account doesn't exist")
		return nil
	}
	if err != nil {
		return err
	}
	deploy.logger.Infof("Deleted the metering serviceaccount")

	return nil
}

func (deploy *Deployer) uninstallMeteringRoleBinding() error {
	res := deploy.config.OperatorResources.RoleBinding
	res.Name = deploy.config.Namespace + "-" + res.Name

	err := deploy.client.RbacV1().RoleBindings(deploy.config.Namespace).Delete(context.TODO(), res.Name, metav1.DeleteOptions{})
	if apierrors.IsNotFound(err) {
		deploy.logger.Warnf("The %s metering RoleBinding resource in the %s namespace doesn't exist", res.Name, deploy.config.Namespace)
		return nil
	}
	if err != nil {
		return err
	}
	deploy.logger.Infof("Deleted the %s metering RoleBinding resource in the %s namespace", res.Name, deploy.config.Namespace)

	return nil
}

func (deploy *Deployer) uninstallMeteringRole() error {
	res := deploy.config.OperatorResources.Role
	res.Name = deploy.config.Namespace + "-" + res.Name

	err := deploy.client.RbacV1().Roles(deploy.config.Namespace).Delete(context.TODO(), res.Name, metav1.DeleteOptions{})
	if apierrors.IsNotFound(err) {
		deploy.logger.Warnf("The %s metering Role resource in the %s namespace doesn't exist", res.Name, deploy.config.Namespace)
		return nil
	}
	if err != nil {
		return err
	}
	deploy.logger.Infof("Deleted the %s metering Role resource in the %s namespace", res.Name, deploy.config.Namespace)

	return nil
}

func (deploy *Deployer) uninstallMeteringClusterRole() error {
	res := deploy.config.OperatorResources.ClusterRole
	res.Name = deploy.config.Namespace + "-" + res.Name

	err := deploy.client.RbacV1().ClusterRoles().Delete(context.TODO(), res.Name, metav1.DeleteOptions{})
	if apierrors.IsNotFound(err) {
		deploy.logger.Warnf("The %s metering ClusterRole resource doesn't exist", res.Name)
		return nil
	}
	if err != nil {
		return err
	}
	deploy.logger.Infof("Deleted the %s metering ClusterRole resource", res.Name)

	return nil
}

func (deploy *Deployer) uninstallReportingOperatorClusterRole() error {
	labelSelector := fmt.Sprintf("app=reporting-operator,metering.openshift.io/ns-prune=%s", deploy.config.Namespace)

	// Attempt to delete all of the ClusterRoles that the metering-ansible-operator
	// creates for the reporting-operator
	crs, err := deploy.client.RbacV1().ClusterRoles().List(context.TODO(), metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return fmt.Errorf("failed to list all the reporting-operator ClusterRoles resources: %v", err)
	}

	if len(crs.Items) == 0 {
		deploy.logger.Warnf("Failed to find any 'app=reporting-operator' ClusterRole resources")
		return nil
	}

	var errArr []string
	for _, cr := range crs.Items {
		err = deploy.client.RbacV1().ClusterRoles().Delete(context.TODO(), cr.Name, metav1.DeleteOptions{})
		if err != nil {
			errArr = append(errArr, fmt.Sprintf("failed to delete the %s ClusterRole resource: %v", cr.Name, err))
		}
		deploy.logger.Infof("Deleted the %s ClusterRole resource", cr.Name)
	}

	if len(errArr) != 0 {
		return fmt.Errorf(strings.Join(errArr, "\n"))
	}

	return nil
}

func (deploy *Deployer) uninstallMeteringClusterRoleBinding() error {
	res := deploy.config.OperatorResources.ClusterRoleBinding
	res.Name = deploy.config.Namespace + "-" + res.Name

	err := deploy.client.RbacV1().ClusterRoleBindings().Delete(context.TODO(), res.Name, metav1.DeleteOptions{})
	if apierrors.IsNotFound(err) {
		deploy.logger.Warnf("The %s metering ClusterRoleBinding resource doesn't exist", res.Name)
		return nil
	}
	if err != nil {
		return err
	}
	deploy.logger.Infof("Deleted the %s metering ClusterRoleBinding resource", res.Name)

	return nil
}

func (deploy *Deployer) uninstallReportingOperatorClusterRoleBinding() error {
	labelSelector := fmt.Sprintf("app=reporting-operator,metering.openshift.io/ns-prune=%s", deploy.config.Namespace)

	// attempt to delete any of the clusterroles the reporting-operator creates
	crbs, err := deploy.client.RbacV1().ClusterRoleBindings().List(context.TODO(), metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return fmt.Errorf("failed to list all the 'app=reporting-operator' ClusterRoleBindings: %v", err)
	}

	if len(crbs.Items) == 0 {
		deploy.logger.Warnf("Failed to find any 'app=reporting-operator' ClusterRoleBinding resources")
		return nil
	}

	var errArr []string
	for _, crb := range crbs.Items {
		err = deploy.client.RbacV1().ClusterRoleBindings().Delete(context.TODO(), crb.Name, metav1.DeleteOptions{})
		if err != nil {
			errArr = append(errArr, fmt.Sprintf("failed to delete the %s ClusterRoleBinding resource: %v", crb.Name, err))
		}
		deploy.logger.Infof("Deleted the %s ClusterRoleBinding resource", crb.Name)
	}
	if len(errArr) != 0 {
		return fmt.Errorf(strings.Join(errArr, "\n"))
	}

	return nil
}

func (deploy *Deployer) uninstallMeteringCRDs() error {
	for _, crd := range deploy.config.OperatorResources.CRDs {
		err := deploy.uninstallMeteringCRD(crd)
		if err != nil {
			return err
		}
	}

	return nil
}

func (deploy *Deployer) uninstallMeteringCRD(resource CRD) error {
	err := deploy.apiExtClient.CustomResourceDefinitions().Delete(context.TODO(), resource.Name, metav1.DeleteOptions{})
	if apierrors.IsNotFound(err) {
		deploy.logger.Warnf("The %s CRD doesn't exist", resource.Name)
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to remove the %s CRD: %v", resource.Name, err)
	}
	deploy.logger.Infof("Deleted the %s CRD", resource.Name)

	return nil
}
