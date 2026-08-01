-- MySQL dump 10.13  Distrib 8.0.46, for Linux (x86_64)
--
-- Host: localhost    Database: SRO_ACCOUNT
-- ------------------------------------------------------
-- Server version	8.0.46

/*!40101 SET @OLD_CHARACTER_SET_CLIENT=@@CHARACTER_SET_CLIENT */;
/*!40101 SET @OLD_CHARACTER_SET_RESULTS=@@CHARACTER_SET_RESULTS */;
/*!40101 SET @OLD_COLLATION_CONNECTION=@@COLLATION_CONNECTION */;
/*!50503 SET NAMES utf8mb4 */;
/*!40103 SET @OLD_TIME_ZONE=@@TIME_ZONE */;
/*!40103 SET TIME_ZONE='+00:00' */;
/*!40014 SET @OLD_UNIQUE_CHECKS=@@UNIQUE_CHECKS, UNIQUE_CHECKS=0 */;
/*!40014 SET @OLD_FOREIGN_KEY_CHECKS=@@FOREIGN_KEY_CHECKS, FOREIGN_KEY_CHECKS=0 */;
/*!40101 SET @OLD_SQL_MODE=@@SQL_MODE, SQL_MODE='NO_AUTO_VALUE_ON_ZERO' */;
/*!40111 SET @OLD_SQL_NOTES=@@SQL_NOTES, SQL_NOTES=0 */;

--
-- Current Database: `SRO_ACCOUNT`
--

CREATE DATABASE /*!32312 IF NOT EXISTS*/ `SRO_ACCOUNT` /*!40100 DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci */ /*!80016 DEFAULT ENCRYPTION='N' */;

USE `SRO_ACCOUNT`;

--
-- Table structure for table `NOTICE`
--

DROP TABLE IF EXISTS `NOTICE`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `NOTICE` (
  `id` int NOT NULL AUTO_INCREMENT,
  `SUBJECT` varchar(80) DEFAULT NULL,
  `ARTICLE` varchar(1024) DEFAULT NULL,
  `CTIME` timestamp NULL DEFAULT NULL,
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
/*!40101 SET character_set_client = @saved_cs_client */;

--
-- Dumping data for table `NOTICE`
--

LOCK TABLES `NOTICE` WRITE;
/*!40000 ALTER TABLE `NOTICE` DISABLE KEYS */;
/*!40000 ALTER TABLE `NOTICE` ENABLE KEYS */;
UNLOCK TABLES;

--
-- Table structure for table `SHARD`
--

DROP TABLE IF EXISTS `SHARD`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `SHARD` (
  `ID` int NOT NULL AUTO_INCREMENT,
  `CONTENT_ID` int NOT NULL DEFAULT '22',
  `NAME` varchar(32) NOT NULL,
  `CAPACITY` int NOT NULL DEFAULT '1000',
  `STATUS` tinyint NOT NULL,
  `ONLINE_PLAYERS` int NOT NULL DEFAULT '0',
  PRIMARY KEY (`ID`)
) ENGINE=InnoDB AUTO_INCREMENT=2 DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
/*!40101 SET character_set_client = @saved_cs_client */;

--
-- Dumping data for table `SHARD`
--

LOCK TABLES `SHARD` WRITE;
/*!40000 ALTER TABLE `SHARD` DISABLE KEYS */;
INSERT INTO `SHARD` VALUES (1,22,'GoSRO',1000,1,0);
/*!40000 ALTER TABLE `SHARD` ENABLE KEYS */;
UNLOCK TABLES;

--
-- Table structure for table `USER`
--

DROP TABLE IF EXISTS `USER`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `USER` (
  `id` int NOT NULL AUTO_INCREMENT,
  `USERNAME` varchar(45) NOT NULL,
  `PASSWORD` varchar(60) DEFAULT NULL,
  `MAIL` varchar(45) DEFAULT NULL,
  `STATUS` int DEFAULT NULL,
  `IS_GM` int DEFAULT NULL,
  `CTIME` timestamp NULL DEFAULT NULL,
  `UTIME` timestamp NULL DEFAULT NULL,
  `USERNAME_NORMALIZED` varchar(45) GENERATED ALWAYS AS (lower(trim(`USERNAME`))) STORED,
  PRIMARY KEY (`id`),
  UNIQUE KEY `UX_USER_USERNAME_NORMALIZED` (`USERNAME_NORMALIZED`)
) ENGINE=InnoDB AUTO_INCREMENT=5 DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
/*!40101 SET character_set_client = @saved_cs_client */;

--
-- Dumping data for table `USER`
--

LOCK TABLES `USER` WRITE;
/*!40000 ALTER TABLE `USER` DISABLE KEYS */;
INSERT INTO `USER` (`id`, `USERNAME`, `PASSWORD`, `MAIL`, `STATUS`, `IS_GM`, `CTIME`, `UTIME`) VALUES (2,'test','$2a$12$EUJB.2CIa0E1AsXJXnhaWO0JSWlcrFXZfbDXVZeibL6kYgZ0Yzc9C','test@test.de',1,1,'2020-02-16 11:25:11',NULL),(4,'test2','$2a$12$EUJB.2CIa0E1AsXJXnhaWO0JSWlcrFXZfbDXVZeibL6kYgZ0Yzc9C','test@test.de',1,1,'2020-02-16 11:25:11',NULL);
/*!40000 ALTER TABLE `USER` ENABLE KEYS */;
UNLOCK TABLES;

--
-- Table structure for table `go_sro_schema_migrations`
--

DROP TABLE IF EXISTS `go_sro_schema_migrations`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `go_sro_schema_migrations` (
  `version` bigint unsigned NOT NULL,
  `name` varchar(255) NOT NULL,
  `checksum` char(64) NOT NULL,
  `state` varchar(16) NOT NULL,
  `applied_at` timestamp(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (`version`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
/*!40101 SET character_set_client = @saved_cs_client */;

--
-- Dumping data for table `go_sro_schema_migrations`
--

LOCK TABLES `go_sro_schema_migrations` WRITE;
/*!40000 ALTER TABLE `go_sro_schema_migrations` DISABLE KEYS */;
INSERT INTO `go_sro_schema_migrations` VALUES (1,'normalize_username','5c8e7e637ff56b58ca1211dacf69fc3df00d02af5dc63a4f8d6e3dde16e2eb98','applied','2026-07-16 19:24:22.003331');
/*!40000 ALTER TABLE `go_sro_schema_migrations` ENABLE KEYS */;
UNLOCK TABLES;

--
-- Dumping events for database 'SRO_ACCOUNT'
--

--
-- Dumping routines for database 'SRO_ACCOUNT'
--
/*!40103 SET TIME_ZONE=@OLD_TIME_ZONE */;

/*!40101 SET SQL_MODE=@OLD_SQL_MODE */;
/*!40014 SET FOREIGN_KEY_CHECKS=@OLD_FOREIGN_KEY_CHECKS */;
/*!40014 SET UNIQUE_CHECKS=@OLD_UNIQUE_CHECKS */;
/*!40101 SET CHARACTER_SET_CLIENT=@OLD_CHARACTER_SET_CLIENT */;
/*!40101 SET CHARACTER_SET_RESULTS=@OLD_CHARACTER_SET_RESULTS */;
/*!40101 SET COLLATION_CONNECTION=@OLD_COLLATION_CONNECTION */;
/*!40111 SET SQL_NOTES=@OLD_SQL_NOTES */;

-- Dump completed on 2026-07-16 19:25:17
